package kcws

import (
	"context"
	"log"
	"sync"
	"time"

	kiteconnect "github.com/zerodha/gokiteconnect/v4"
	kitemodels "github.com/zerodha/gokiteconnect/v4/models"
	kiteticker "github.com/zerodha/gokiteconnect/v4/ticker"

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
		log.Println("WebSocket connected, resubscribing to tokens...")
		k.ReSubscribeTokens()
		log.Println("WebSocket connected")
	})

	ws.OnClose(func(code int, reason string) {
		k.isConnected = false
		log.Printf("WebSocket closed: code=%d, reason=%s", code, reason)
	})  
    
	ws.OnError(func(err error) {
		k.isConnected = false
		log.Printf("WebSocket error: %v", err)
	})

	// ws.OnMessage(func (messageType int, message []byte){
	// 	log.Printf("WebSocket message received: type=%d, message=%s", messageType, string(message))
	// })

	ws.OnReconnect(func(attempt int, delay time.Duration) {
		log.Printf("Websocket reconnecting attempt: %d, delay: %d", attempt, delay)
	})

	ws.OnOrderUpdate(func(order kiteconnect.Order) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		if err := k.OrderSvc.ProcessOrderUpdate(ctx, order); err != nil {
			log.Println(err)
		}
	})

	return k, nil
}

func (kws *KiteWS) Start() {
	go kws.ws.Serve()
}

func (kws *KiteWS) Stop() {
	kws.ws.Close()
}

func (kws *KiteWS) SubscribeToken(token uint32) {
	kws.mu.Lock()
	defer kws.mu.Unlock()

	if _, exists := kws.tokens[token]; exists {
		return
	}

	if !kws.isConnected {
		log.Printf("⚠️ WebSocket not connected, queuing instrument_token %d", token)
		kws.tokens[token] = struct{}{}
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

func (kws *KiteWS) ReSubscribeTokens() {
	kws.mu.Lock()
	defer kws.mu.Unlock()

	if len(kws.tokens) == 0 {
		return
	}

	tokens := make([]uint32, 0, len(kws.tokens))
	for token := range kws.tokens {
		tokens = append(tokens, token)
	}
	kws.ws.Subscribe(tokens)
	kws.ws.SetMode(kiteticker.ModeQuote, tokens)
}

func (kws *KiteWS) IsConnected() bool {
	kws.mu.Lock()
	defer kws.mu.Unlock()
	return kws.isConnected
}
