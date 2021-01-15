# cellaut: cellular automata simulation framework

no matter what i do it feels like i'm assigning each NeighborIO twice, and in `Channels` i don't
know if it's been assigned yet. the process has got to be deterministic, so understand it and do it
right.

what if instead of channels we have the CellAuts hold pointers to each other? then it's 

```
// explanation of outer/inner grid system
iterate over inner grid {
  grid[y*nx+x].AddNeighbor(NeighborUp, grid[(y+1)*nx+x])
  grid[y*nx+x].AddNeighbor(NeighborRt, grid[y*nx+x+1])
  grid[y*nx+x].AddNeighbor(NeighborDn, grid[(y-1)*nx+x])
  grid[y*nx+x].AddNeighbor(NeighborLf, grid[y*nx+x-1])
}

// Tells the TronCellAut "this is your neighbor in this direction.
//
// The index should be our Neighbor?? constants
func (aut *TronCellAut) AddNeighbor(index int, aut CellAut) {
  aut.Neighbors[index] = aut
}

func (aut *TronCellAut) Start(tick ...) {
  // on state change
  aut.Neighbors[NeighborUp] <- aut.state
}
```

## race condition

i think we have a nondeterministic situation, because we have no guarantee that when we ask the
CellAuts for their states, the tick is over. `ticker.Tick()` sends the signals on the channels, and
they're unbuffered channels so we know that the tick signal has been received by all the CellAuts
when Tick() returns. but with `Start()` running in goroutines, there's a race condition where the
test might call `GetState()` before a CellAut is finished receiving its update states from its
neighbors, which might update _their_ states.

have to nail down the `Tick()` semantics. maybe it should be `GetState(tickID uint64)`, and
`GetState()` can block until the tick with that ID is finished processing. but does a given CellAut
_know_ that a given tick is finished processing? no, not really, because sometimes it gets messages
from its neighbors and sometimes it doesn't. so it wouldn't know when to unblock.

we could have CellAuts _always_ send their states to their neighbors, even if it hasn't changed. but
that feels like a big bottleneck. … maybe. i mean there are probably things we'll implement with
this framework where state will necessarily be transmitted by every CellAut on every tick. but i
would prefer not to have that be required.

we could have `Start()` send a message to some channel that means "i'm done sending signals to my
neighbors", and then put all those channels in a `sync.WaitGroup`. that means passing an extra thing
to `Start()`, which is going to need a `done` channel too. so 4 channels total. that seems like too
many.

but i think that's the path of least resistance. i'll try it.

## why is this test failing?

are we sure that all the CellAuts are done receiving neighbor signals and updating their state
before the next Tick starts? `tickDone` gets written to after the CellAut _sends_ all its signals,
but the channels are buffered. could there still be CellAuts that are processing the signals that
they were sent?

oh y'know what i bet is happening? okay 2 possibilities:

1. `concatStates()` calls `aut.GetState()` which returns the state that the aut will have _after_
   the next tick, rather than its current state
2. there's a race condition: sometimes an `aut` gets the state update message from its neighbor
   before it gets the tick signal. so by the time it gets tick, its state has already changed and it
   decides it has to push that new state to its neighbor. that's why sometimes we get 4=X but
   sometimes we don't.

solution:

* SetState() sets aut.nextState, not aut.state
* `<-tick` does `wg.Add(len(aut.toNeighbors))`; `<-fromNeighbors[*]` does `wg.Done()`. but then we
    have to give the `aut`s that `sync.WaitGroup`. and can we reuse a `sync.WaitGroup()`? yes.

there's still a problem, which is that we can't guarantee that _any_ of the `wg.Add()` calls have
been reached before we get to the `wg.Wait()`.

what we want is to `wg.Add(1)` each time we send a message to a neighbor, and `wg.Done()` each time
we receive a message from a neighbor. but the thing that is calling `wg.Wait()` doesn't know whether
any messages have been sent yet, so it calls `wg.Wait()` naively and depending on
timing/concurrency, it may do so before any `wg.Add(1)` calls.

we need to make sure at least one `wg.Add()` call happens before the `wg.Wait()`.

one thing we could do is have the calling code trigger aut ticks with an `aut.Tick()` function
instead of messaging a `tick` channel. that way `aut.Tick()` could block. blech though. performance.

could we have `aut.Tick()` return a `*sync.WaitGroup` and have the calling code call `Wait()` on
it?? hmm, but if we don't have the `WaitGroup` until `Tick()` is called, then we can't pass it to
the `aut`s' `Start` methods. Unless we don't call `Start()` until after the first `Tick()`. let's
try that. -> beh no that doesn't make sense either. `wg.Add(1)` needs to be called when we send a
message to a neighbor, which doesn't happen in `Tick()`.

we need to rearrange the whole interface.

## more stuff to do

buffer all channels 1 to prevent blocking

also worry about directionality after tests are passing

take inventory after a test is passing

separate done channel

## deadlock debugging 2020-01-10

```
goroutine   aut.ID  line              event
----------|--------|-----------------|-------------------------
21          0       main.go:163       aut.Start()   tickDone<-
22          1       main.go:163       aut.Start()   tickDone<-
24          3       main.go:163       aut.Start()   tickDone<-
23          2       main.go:163       aut.Start()   tickDone<-
25          4       main.go:163       aut.Start()   tickDone<-
7           4 (i)   main_test.go:50   ticker.Tick() <-ticker.tickDones[i]
9           4 (i)   main_test.go:50   ticker.Tick() <-ticker.tickDones[i]
6           4 (i)   main_test.go:50   ticker.Tick() <-ticker.tickDones[i]
8           4 (i)   main_test.go:50   ticker.Tick() <-ticker.tickDones[i]
```

fixed the timeouts. it ended up, i think, being a matter of changing

```
	for i, ch := range ticker.tickDones {
		//@DEBUG ^
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-ch
		}()
	}
```

to

```
	for i, ch := range ticker.tickDones {
		//@DEBUG ^
		wg.Add(1)
		go func(ch chan int64) {
			// pass the channel instead of pulling it in from the scope of Tick(). very important
			// because otherwise the code in the goroutines will all end up using the max value from
			// the loop.
			//
			// this made me very sad in early 2021. do not repeat my mistakes
			defer wg.Done()
			<-ch
		}(i, ch)
	}
```
