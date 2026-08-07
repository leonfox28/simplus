package realtime

import (
	"testing"
	"time"
)

func TestHubStartsWithResyncAndPublishesBoundedUpdate(t *testing.T) {
	hub := NewHub()
	subscription := hub.Subscribe()
	defer subscription.Close()
	initial := <-subscription.C
	if initial.Kind != KindResync || len(initial.Topics) != len(allTopics) {
		t.Fatalf("initial event = %#v", initial)
	}
	hub.Publish([]Topic{TopicMessages, TopicMessages}, AttentionSMSReceived)
	event := <-subscription.C
	if event.Kind != KindUpdate || len(event.Topics) != 1 || event.Topics[0] != TopicMessages || event.Attention != AttentionSMSReceived {
		t.Fatalf("published event = %#v", event)
	}
}

func TestSlowSubscriberCollapsesToResyncWithoutBlockingPublisher(t *testing.T) {
	hub := NewHub()
	subscription := hub.Subscribe()
	defer subscription.Close()
	done := make(chan struct{})
	go func() {
		for index := 0; index < 10_000; index++ {
			hub.Publish([]Topic{TopicCalls}, AttentionCallIncoming)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("publisher blocked on a slow subscriber")
	}
	event := <-subscription.C
	if event.Kind != KindResync || event.Attention != "" || len(event.Topics) != len(allTopics) {
		t.Fatalf("collapsed event = %#v", event)
	}
}

func TestSubscriptionCloseCleansUp(t *testing.T) {
	hub := NewHub()
	subscription := hub.Subscribe()
	<-subscription.C
	subscription.Close()
	if _, ok := <-subscription.C; ok {
		t.Fatal("subscription channel remained open")
	}
	hub.Publish([]Topic{TopicSystem}, "")
}
