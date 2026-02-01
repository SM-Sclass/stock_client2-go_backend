package kcws

import (
	kitemodels "github.com/zerodha/gokiteconnect/v4/models"
	kiteticker "github.com/zerodha/gokiteconnect/v4/ticker"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
	"sync"
	"context"
	"log"
	"time"

	"github.com/SM-Sclass/stock_client2-go_backend/internal/kite"
	"github.com/SM-Sclass/stock_client2-go_backend/internal/services"
)

type KiteWS struct {
	ws          *kiteticker.Ticker
	tokens      map[uint32]struct{}
	mu          sync.Mutex
	bus         *kite.TickBroadcaster
	OrderSvc    *services.OrderService
	isConnected bool
}

func NewKiteWS(kc *kite.KiteClient, bus *kite.TickBroadcaster, orderService *services.OrderService) (*KiteWS, error) {
	ws := kiteticker.New(kc.APIKey, kc.AccessToken)

	k := &KiteWS{
		ws:          ws,
		tokens:      make(map[uint32]struct{}),
		bus:         bus,
		OrderSvc:    orderService,
		isConnected: false,
	}

	ws.OnTick(func(tick kitemodels.Tick) {
		k.bus.Broadcast([]kitemodels.Tick{tick})
	})

	ws.OnConnect(func() {
		k.isConnected = true
	})

	ws.OnOrderUpdate(func(order kiteconnect.Order){
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
    defer cancel()

    if err := k.OrderSvc.ProcessOrderUpdate(ctx, order); err != nil {
        log.Println(err)
    }
	})

	return k, nil
}

func (kws *KiteWS) SubscribeToken(token uint32) {
	kws.mu.Lock()
	defer kws.mu.Unlock()

	if _, exists := kws.tokens[token]; exists {
		return
	}

	kws.tokens[token] = struct{}{}
	kws.ws.Subscribe([]uint32{token})
	kws.ws.SetMode(kiteticker.ModeQuote, []uint32{token})
}

func (kws *KiteWS) UnsubscribeToken(token uint32) {
	kws.mu.Lock()
	defer kws.mu.Unlock()

	if _, exists := kws.tokens[token]; !exists {
		return
	}

	delete(kws.tokens, token)
	kws.ws.Unsubscribe([]uint32{token})
}
