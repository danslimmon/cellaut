package main

import (
	"os"
	"sync"

	log "github.com/Sirupsen/logrus"
)

const (
	NeighborUp NeighborIndex = 0
	NeighborRt NeighborIndex = 1
	// NeighborDn = ^ NeighborUp = NeighborUp.Recip()
	NeighborDn NeighborIndex = 255
	// NeighborLf = ^ NeighborRt = NeighborRt.Recip()
	NeighborLf NeighborIndex = 254
)

type State string

type NeighborIndex uint8

/*
Recip returns the reciprocal of the NeighborIndex (i.e. the opposite direction)

For example, NeighborUp.Recip() = NeighborDn. NeighborDn.Recip() = NeighborUp. Etc.
*/
func (i NeighborIndex) Recip() NeighborIndex {
	return ^i
}

type Ticker struct {
	tickID       int64
	destinations []chan int64
	waitGroup    sync.WaitGroup
}

func (ticker *Ticker) TickChan() chan int64 {
	newChan := make(chan int64)
	ticker.destinations = append(ticker.destinations, newChan)
	return newChan
}

func (ticker *Ticker) Tick() {
	// Wait at least until all destinations have called their `tickProcessed()`
	// callbacks.
	ticker.waitGroup.Add(len(ticker.destinations))
	for _, dest := range ticker.destinations {
		dest <- ticker.tickID
	}
	ticker.waitGroup.Wait()
	ticker.tickID++
}

func (ticker *Ticker) Callbacks() *CellAutCallbacks {
	return &CellAutCallbacks{WaitGroup: &ticker.waitGroup}
}

type CellAutCallbacks struct {
	WaitGroup *sync.WaitGroup
}

func (callbacks *CellAutCallbacks) StateSent() {
	callbacks.WaitGroup.Add(1)
}

func (callbacks *CellAutCallbacks) StateReceived() {
	callbacks.WaitGroup.Done()
}

func (callbacks *CellAutCallbacks) AllStatesSent() {
	callbacks.WaitGroup.Done()
}

/*
CellAut is the interface that cellular automata implement.
*/
type CellAut interface {
	// AddNeighbor introduces the CellAut to its neighbor.
	//
	// This causes the CellAut on which AddNeighbor was called to populate its NeighborIO such that
	// it knows how to transmit states to and from aut.
	//
	// index should be one of the `Neighbor*` constants.
	AddNeighbor(i NeighborIndex, aut CellAut)

	// Channels returns a channel that can be used to send States to the CellAut and a channel on
	// which it will send States to other CellAuts.
	//
	// recipIndex is the relationship that the callee has to the caller. For example,
	// aut.Channels(NeighborDn) means "You are my NeighborDn. Return the channels I should use to
	// talk to you."
	Channels(recipIndex NeighborIndex) (to, from chan State)

	// Start brings the CellAut to life. It should be called as a goroutine.
	//
	// The `tick` channel receives a random int64 value at every tick of the clock. The `tick`
	// channel is closed .
	Start(tick chan int64, done chan struct{}, stateLedger chan State, callbacks *CellAutCallbacks)

	// Returns the current state of the CellAut.
	//
	// This state may not yet have been transmitted to neighbors, depending on where we are in the
	// tick cycle.
	GetState() State

	// Sets the state of CellAut.
	//
	// This state will be transmitted to neighbors at the next tick.
	//
	// SetState is the only way a CellAut's state should ever get set.
	SetState(State)
}

/*
GooCellAut is a CellAut implementation that spreads one tick at a time to every adjacent neighbor.

It has two states, "X" and "-". "X" means "covered in goo", "-" means "not (yet) covered in goo".
*/
type GooCellAut struct {
	//@DEBUG
	ID int
	// The next state the GooCellAut will have (after the next tick)
	newState State
	// The current state of the GooCellAut
	state State
	// The channels on which we send states to our neighbors
	toNeighbors map[NeighborIndex]chan State
	// The channels on which we receive states from our neighbors
	fromNeighbors map[NeighborIndex]chan State
}

/*
AddNeighbor tells us "your neighbor to this direction is `neighbor`".

We call that neighbor's Channels() to get its To and From channels and save them.
*/
func (aut *GooCellAut) AddNeighbor(i NeighborIndex, neighbor CellAut) {
	toNeighbor, fromNeighbor := neighbor.Channels(i)
	aut.toNeighbors[i] = toNeighbor
	aut.fromNeighbors[i] = fromNeighbor
}

/*
Channels returns the channels on which the given neighbor should talk to us.

`reciprocalIndex` is the relationship _we have to the caller_. So, for example,
`Channels(NeighborUp)` returns the channels that our NeighborDn should use to talk to us.
*/
func (aut *GooCellAut) Channels(recipIndex NeighborIndex) (to, from chan State) {
	// recipIndex is the relationship we hold to the neighbor. recipIndex.Recip() is the
	// relationship the neighbor holds to us, so that's the index we use to save the channels.
	neighborIndex := recipIndex.Recip()
	aut.toNeighbors[neighborIndex] = make(chan State, 1)
	aut.fromNeighbors[neighborIndex] = make(chan State, 1)
	// fromNeighbors[neighborIndex] is the channel our `neighborIndex` should use to talk _to_ us.
	// toNeighbors[neighborIndex] is the channel our `neighborIndex` should use to hear _from_ us.
	return aut.fromNeighbors[neighborIndex], aut.toNeighbors[neighborIndex]
}

/*
SetState sets the *GooCellAut's state.

This is the only way state should ever be set on a *GooCellAut.

SetState can be called multiple times per tick. If it is, the last state will win.
*/
func (aut *GooCellAut) SetState(newState State) {
	aut.newState = newState
}

/*
GetState returns the *GooCellAut's state.

Depending where we are in the simulation, this state might be new, and not yet transmitted to the
neighbors.
*/
func (aut *GooCellAut) GetState() State {
	return aut.state
}

func (aut *GooCellAut) Start(tick chan int64, done chan struct{}, stateLedger chan State, callbacks *CellAutCallbacks) {
	var neighborState State
	for {
		select {
		case <-tick:
			if aut.newState != aut.state {
				aut.state = aut.newState
				for _, ch := range aut.toNeighbors {
					callbacks.StateSent()
					ch <- aut.state
				}
			}
			callbacks.AllStatesSent()
		case <-done:
			return
		// there must be some kinda package that lets me collapse these 4 cases
		case neighborState = <-aut.fromNeighbors[NeighborUp]:
			aut.SetState(neighborState)
			callbacks.StateReceived()
		case neighborState = <-aut.fromNeighbors[NeighborRt]:
			aut.SetState(neighborState)
			callbacks.StateReceived()
		case neighborState = <-aut.fromNeighbors[NeighborDn]:
			aut.SetState(neighborState)
			callbacks.StateReceived()
		case neighborState = <-aut.fromNeighbors[NeighborLf]:
			aut.SetState(neighborState)
			callbacks.StateReceived()
		}
	}
}

/*
NewGooCellAut returns a *GooCellAut that has been initialized.

"Initialized" means it's okay to call Channels and AddNeighbor on it.
*/
func NewGooCellAut(i int) *GooCellAut {
	//@DEBUG v^
	aut := &GooCellAut{ID: i}
	aut.toNeighbors = make(map[NeighborIndex]chan State)
	aut.fromNeighbors = make(map[NeighborIndex]chan State)
	return aut
}

func main() {
	logFile, err := os.OpenFile("/Users/danslimmon/cellaut.log", os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		panic(err)
	}
	log.SetOutput(logFile)
}
