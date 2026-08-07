package realtime

import (
	"sort"
	"sync"
)

type Topic string

const (
	TopicSystem        Topic = "system"
	TopicInventory     Topic = "inventory"
	TopicModems        Topic = "modems"
	TopicLines         Topic = "lines"
	TopicVoWiFi        Topic = "vowifi"
	TopicMessages      Topic = "messages"
	TopicCalls         Topic = "calls"
	TopicContacts      Topic = "contacts"
	TopicMihomo        Topic = "mihomo"
	TopicNotifications Topic = "notifications"
	TopicEUICC         Topic = "euicc"
)

var allTopics = []Topic{
	TopicSystem, TopicInventory, TopicModems, TopicLines, TopicVoWiFi,
	TopicMessages, TopicCalls, TopicContacts, TopicMihomo, TopicNotifications, TopicEUICC,
}

type Attention string

const (
	AttentionSMSReceived  Attention = "sms.received"
	AttentionCallIncoming Attention = "call.incoming"
)

type Kind string

const (
	KindUpdate Kind = "update"
	KindResync Kind = "resync"
)

type Event struct {
	ID        uint64
	Kind      Kind
	Topics    []Topic
	Attention Attention
}

type Hub struct {
	mu          sync.Mutex
	nextID      uint64
	nextSubID   uint64
	subscribers map[uint64]chan Event
}

func NewHub() *Hub {
	return &Hub{subscribers: make(map[uint64]chan Event)}
}

type Subscription struct {
	hub  *Hub
	id   uint64
	once sync.Once
	C    <-chan Event
}

func (hub *Hub) Subscribe() *Subscription {
	if hub == nil {
		channel := make(chan Event)
		close(channel)
		return &Subscription{C: channel}
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	hub.nextSubID++
	id := hub.nextSubID
	channel := make(chan Event, 1)
	hub.nextID++
	channel <- Event{ID: hub.nextID, Kind: KindResync, Topics: append([]Topic(nil), allTopics...)}
	hub.subscribers[id] = channel
	return &Subscription{hub: hub, id: id, C: channel}
}

func (subscription *Subscription) Close() {
	if subscription == nil {
		return
	}
	subscription.once.Do(func() {
		if subscription.hub == nil {
			return
		}
		subscription.hub.mu.Lock()
		channel, ok := subscription.hub.subscribers[subscription.id]
		if ok {
			delete(subscription.hub.subscribers, subscription.id)
			close(channel)
		}
		subscription.hub.mu.Unlock()
	})
}

func (hub *Hub) Publish(topics []Topic, attention Attention) {
	if hub == nil {
		return
	}
	normalized, ok := normalizeTopics(topics)
	if !ok || !validAttention(attention) {
		return
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	hub.nextID++
	event := Event{ID: hub.nextID, Kind: KindUpdate, Topics: normalized, Attention: attention}
	for _, subscriber := range hub.subscribers {
		select {
		case subscriber <- event:
		default:
			// A lagging browser needs only one bounded resync hint. Discarding
			// attention is safe because authoritative records remain queryable.
			select {
			case <-subscriber:
			default:
			}
			subscriber <- Event{ID: hub.nextID, Kind: KindResync, Topics: append([]Topic(nil), allTopics...)}
		}
	}
}

func normalizeTopics(topics []Topic) ([]Topic, bool) {
	if len(topics) == 0 || len(topics) > len(allTopics) {
		return nil, false
	}
	allowed := make(map[Topic]struct{}, len(allTopics))
	for _, topic := range allTopics {
		allowed[topic] = struct{}{}
	}
	unique := make(map[Topic]struct{}, len(topics))
	for _, topic := range topics {
		if _, ok := allowed[topic]; !ok {
			return nil, false
		}
		unique[topic] = struct{}{}
	}
	result := make([]Topic, 0, len(unique))
	for topic := range unique {
		result = append(result, topic)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result, true
}

func validAttention(attention Attention) bool {
	return attention == "" || attention == AttentionSMSReceived || attention == AttentionCallIncoming
}
