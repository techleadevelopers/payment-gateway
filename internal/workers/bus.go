package workers

import (
	"sync"
)

// Event representa a estrutura genérica de mensagens no nosso barramento interno
type Event struct {
	Type    string
	OrderID string
	Payload map[string]interface{}
}

// EventBus gerencia a distribuição de eventos para os workers concorrentes
type EventBus struct {
	mu          sync.RWMutex
	subscribers map[string][]chan Event
}

// NewEventBus inicializa o barramento de eventos
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[string][]chan Event),
	}
}

// Subscribe registra um canal para escutar um tipo específico de evento (Equivalente ao queue.subscribe)
func (b *EventBus) Subscribe(eventType string) chan Event {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Criamos un canal com buffer de 100 eventos para evitar travamento (backpressure control)
	ch := make(chan Event, 100)
	b.subscribers[eventType] = append(b.subscribers[eventType], ch)
	return ch
}

// Publish envia um evento para todos os inscritos daquele canal (Equivalente ao queue.publish)
func (b *EventBus) Publish(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	subs, exists := b.subscribers[event.Type]
	if !exists {
		return
	}

	for _, ch := range subs {
		// Envio não-bloqueante para proteger a latência do sistema principal
		select {
		case ch <- event:
		default:
			// Se o buffer do canal estiver cheio, o evento é dropado ou logado para evitar estouro de memória
		}
	}
}