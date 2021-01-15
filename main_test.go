package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

/*
Returns a list consisting of the concatStates of each of the auts in the input.
*/
func concatStates(auts []CellAut) string {
	var rslt string
	for _, aut := range auts {
		state := string(aut.GetState())
		if state == "" {
			state = "-"
		}
		rslt = rslt + state
	}
	return rslt
}

/*
Tests the functionality of GooCellAut, which itself is used only for testing.
*/
func TestGooCellAut(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)

	// Put five GooCellAuts in a row. Set the middle one to gooed. After the next tick, there should
	// be 3 gooed. After the next tick, all 5 should be gooed.
	//
	// After tick 0: --X--
	// After tick 1: -XXX-
	// After tick 2: XXXXX
	auts := make([]CellAut, 5)
	for i := range auts {
		auts[i] = NewGooCellAut(i)
	}
	auts[0].AddNeighbor(NeighborRt, auts[1])
	auts[1].AddNeighbor(NeighborLf, auts[0])
	auts[1].AddNeighbor(NeighborRt, auts[2])
	auts[2].AddNeighbor(NeighborLf, auts[1])
	auts[2].AddNeighbor(NeighborRt, auts[3])
	auts[2].SetState("X")
	auts[3].AddNeighbor(NeighborLf, auts[2])
	auts[3].AddNeighbor(NeighborRt, auts[4])
	auts[4].AddNeighbor(NeighborLf, auts[3])
	ticker := &Ticker{}
	stateLedger := make(chan State)
	done := make(chan struct{})
	defer close(done)
	callbacks := ticker.Callbacks()
	for _, aut := range auts {
		tickChan := ticker.TickChan()
		go aut.Start(tickChan, done, stateLedger, callbacks)
	}
	go func() {
		// Discard everything sent to state ledger
		for {
			_ = <-stateLedger
		}
	}()
	ticker.Tick()
	assert.Equal("--X--", concatStates(auts))
	ticker.Tick()
	assert.Equal("-XXX-", concatStates(auts))
	ticker.Tick()
	assert.Equal("XXXXX", concatStates(auts))
	// After they're gooed, cells should stay gooed
	for i := 0; i < 10; i++ {
		ticker.Tick()
	}
	assert.Equal("XXXXX", concatStates(auts))
}
