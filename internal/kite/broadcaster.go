package kite

import (
	kitemodels "github.com/zerodha/gokiteconnect/v4/models"
)

type TickBroadcaster struct {
	subscribers []chan []kitemodels.Tick
}

func NewTickBroadcaster() *TickBroadcaster {
	return &TickBroadcaster{
		subscribers: make([]chan []kitemodels.Tick, 0),
	}
}

func (b *TickBroadcaster) Subscribe(buffer int) chan []kitemodels.Tick {
	ch := make(chan []kitemodels.Tick, buffer)
	b.subscribers = append(b.subscribers, ch)
	return ch
}

func (b *TickBroadcaster) Broadcast(ticks []kitemodels.Tick) {
	for _, sub := range b.subscribers {
		select {
		case sub <- ticks:
		default:
			// drop if slow
		}
	}
}
