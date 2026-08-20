package messaging

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/application/realtime"
)

type syncTestOutcome struct {
	result InboundSyncResult
	err    error
}

type fakeInboundSyncer struct {
	outcomes []syncTestOutcome
	calls    int
	events   *[]string
	inspect  func(context.Context)
}

func (fake *fakeInboundSyncer) SyncInbound(ctx context.Context) (InboundSyncResult, error) {
	if fake.events != nil {
		*fake.events = append(*fake.events, "sync")
	}
	if fake.inspect != nil {
		fake.inspect(ctx)
	}
	if fake.calls >= len(fake.outcomes) {
		return InboundSyncResult{}, errors.New("unexpected synchronization call")
	}
	outcome := fake.outcomes[fake.calls]
	fake.calls++
	return outcome.result, outcome.err
}

type fakeNotificationSender struct {
	receivedErrors []error
	summaryError   error
	receivedCalls  []receivedSMSNotification
	summaryCalls   []int
	contexts       []context.Context
	events         *[]string
	inspect        func(context.Context)
}

func (fake *fakeNotificationSender) NotifyReceivedSMS(ctx context.Context, sender, body string) error {
	callIndex := len(fake.receivedCalls)
	fake.receivedCalls = append(fake.receivedCalls, receivedSMSNotification{Sender: sender, Body: body})
	fake.contexts = append(fake.contexts, ctx)
	if fake.events != nil {
		*fake.events = append(*fake.events, "notify:sms")
	}
	if fake.inspect != nil {
		fake.inspect(ctx)
	}
	if callIndex < len(fake.receivedErrors) {
		return fake.receivedErrors[callIndex]
	}
	return nil
}

func (fake *fakeNotificationSender) NotifyReceivedSMSSummary(ctx context.Context, count int) error {
	fake.summaryCalls = append(fake.summaryCalls, count)
	fake.contexts = append(fake.contexts, ctx)
	if fake.events != nil {
		*fake.events = append(*fake.events, "notify:summary")
	}
	if fake.inspect != nil {
		fake.inspect(ctx)
	}
	return fake.summaryError
}

type syncTestPublication struct {
	topics    []realtime.Topic
	attention realtime.Attention
}

type fakeSyncPublisher struct {
	publications []syncTestPublication
	events       *[]string
}

func (fake *fakeSyncPublisher) Publish(topics []realtime.Topic, attention realtime.Attention) {
	copyOfTopics := append([]realtime.Topic(nil), topics...)
	fake.publications = append(fake.publications, syncTestPublication{topics: copyOfTopics, attention: attention})
	if fake.events != nil {
		*fake.events = append(*fake.events, "publish:"+string(topics[0]))
	}
}

func TestNewSyncCoordinatorRequiresDependencies(t *testing.T) {
	syncer := &fakeInboundSyncer{}
	notifications := &fakeNotificationSender{}
	publisher := &fakeSyncPublisher{}
	for _, test := range []struct {
		name          string
		syncer        InboundSyncer
		notifications NotificationSender
		publisher     RealtimePublisher
	}{
		{name: "missing syncer", notifications: notifications, publisher: publisher},
		{name: "missing notifications", syncer: syncer, publisher: publisher},
		{name: "missing publisher", syncer: syncer, notifications: notifications},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewSyncCoordinator(test.syncer, test.notifications, test.publisher); !errors.Is(err, ErrSyncCoordinatorConfiguration) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestSyncCoordinatorCyclePublishesAndNotifiesInOrder(t *testing.T) {
	var events []string
	parent, cancelParent := context.WithCancel(context.Background())
	cancelParent()
	syncer := &fakeInboundSyncer{
		outcomes: []syncTestOutcome{{result: InboundSyncResult{
			Persisted: 2, Acknowledged: 1,
			receivedSMS: []receivedSMSNotification{
				{Sender: "10086", Body: "first"},
				{Sender: "Service", Body: "second\nline"},
			},
		}}},
		events: &events,
		inspect: func(ctx context.Context) {
			if ctx.Err() == nil {
				t.Fatal("synchronization context did not inherit parent cancellation")
			}
			assertDeadlineNear(t, ctx, syncTimeout)
		},
	}
	notifications := &fakeNotificationSender{
		events: &events,
		inspect: func(ctx context.Context) {
			if err := ctx.Err(); err != nil {
				t.Fatalf("notification context inherited cancellation: %v", err)
			}
			assertDeadlineNear(t, ctx, notificationTimeout)
		},
	}
	publisher := &fakeSyncPublisher{events: &events}
	coordinator, err := NewSyncCoordinator(syncer, notifications, publisher)
	if err != nil {
		t.Fatal(err)
	}

	report := coordinator.runCycle(parent)
	if report.SyncError != nil || report.NotificationError != nil || !report.DurableChange {
		t.Fatalf("report = %#v", report)
	}
	wantReceived := []receivedSMSNotification{{Sender: "10086", Body: "first"}, {Sender: "Service", Body: "second\nline"}}
	if !reflect.DeepEqual(notifications.receivedCalls, wantReceived) || !reflect.DeepEqual(notifications.summaryCalls, []int{2}) {
		t.Fatalf("received calls = %#v, summary calls = %#v", notifications.receivedCalls, notifications.summaryCalls)
	}
	if len(notifications.contexts) != 3 || notifications.contexts[0] == notifications.contexts[1] || notifications.contexts[1] == notifications.contexts[2] {
		t.Fatalf("notification contexts are not independent: %#v", notifications.contexts)
	}
	wantEvents := []string{"sync", "publish:messages", "notify:sms", "notify:sms", "notify:summary", "publish:notifications"}
	if !reflect.DeepEqual(events, wantEvents) {
		t.Fatalf("events = %#v, want %#v", events, wantEvents)
	}
	if len(publisher.publications) != 2 ||
		!reflect.DeepEqual(publisher.publications[0].topics, []realtime.Topic{realtime.TopicMessages}) ||
		publisher.publications[0].attention != realtime.AttentionSMSReceived ||
		!reflect.DeepEqual(publisher.publications[1].topics, []realtime.Topic{realtime.TopicNotifications}) ||
		publisher.publications[1].attention != "" {
		t.Fatalf("publications = %#v", publisher.publications)
	}
}

func TestSyncCoordinatorCycleKeepsPartialProgressAndNotificationFailure(t *testing.T) {
	syncErr := errors.New("partial synchronization")
	notificationErr := errors.New("delivery unavailable")
	syncer := &fakeInboundSyncer{outcomes: []syncTestOutcome{{
		result: InboundSyncResult{
			Persisted: 1, OutboundFailed: 2,
			receivedSMS: []receivedSMSNotification{{Sender: "private-sender", Body: "private body"}},
		},
		err: syncErr,
	}}}
	notifications := &fakeNotificationSender{receivedErrors: []error{notificationErr}}
	publisher := &fakeSyncPublisher{}
	coordinator, err := NewSyncCoordinator(syncer, notifications, publisher)
	if err != nil {
		t.Fatal(err)
	}

	report := coordinator.runCycle(context.Background())
	if !errors.Is(report.SyncError, syncErr) || !errors.Is(report.NotificationError, notificationErr) || !report.DurableChange {
		t.Fatalf("report = %#v", report)
	}
	if len(notifications.receivedCalls) != 1 || len(notifications.summaryCalls) != 1 || len(publisher.publications) != 2 || publisher.publications[0].attention != realtime.AttentionSMSReceived {
		t.Fatalf("received calls = %d, summary calls = %d, publications = %#v", len(notifications.receivedCalls), len(notifications.summaryCalls), publisher.publications)
	}
	if strings.Contains(report.NotificationError.Error(), "private-sender") || strings.Contains(report.NotificationError.Error(), "private body") {
		t.Fatalf("notification error leaked SMS content: %v", report.NotificationError)
	}
}

func TestSyncCoordinatorCycleContinuesAfterIndependentSMSFailure(t *testing.T) {
	firstError := errors.New("first delivery timed out")
	syncer := &fakeInboundSyncer{outcomes: []syncTestOutcome{{result: InboundSyncResult{
		Persisted: 2,
		receivedSMS: []receivedSMSNotification{
			{Sender: "10010", Body: "first"},
			{Sender: "10086", Body: "second"},
		},
	}}}}
	notifications := &fakeNotificationSender{receivedErrors: []error{firstError}}
	publisher := &fakeSyncPublisher{}
	coordinator, err := NewSyncCoordinator(syncer, notifications, publisher)
	if err != nil {
		t.Fatal(err)
	}

	report := coordinator.runCycle(context.Background())
	if !errors.Is(report.NotificationError, firstError) {
		t.Fatalf("notification error = %v", report.NotificationError)
	}
	want := []receivedSMSNotification{{Sender: "10010", Body: "first"}, {Sender: "10086", Body: "second"}}
	if !reflect.DeepEqual(notifications.receivedCalls, want) || !reflect.DeepEqual(notifications.summaryCalls, []int{2}) {
		t.Fatalf("received calls = %#v, summary calls = %#v", notifications.receivedCalls, notifications.summaryCalls)
	}
	if len(publisher.publications) != 2 || publisher.publications[1].topics[0] != realtime.TopicNotifications {
		t.Fatalf("publications = %#v", publisher.publications)
	}
}

func TestSyncCoordinatorCycleClassifiesDurableMessageChanges(t *testing.T) {
	tests := []struct {
		name    string
		result  InboundSyncResult
		changed bool
	}{
		{name: "empty"},
		{name: "acknowledgement only", result: InboundSyncResult{Acknowledged: 1}},
		{name: "outbound report acknowledgement only", result: InboundSyncResult{OutboundReportsAcknowledged: 1}},
		{name: "already known", result: InboundSyncResult{AlreadyKnown: 1}, changed: true},
		{name: "outbound sent", result: InboundSyncResult{OutboundSent: 1}, changed: true},
		{name: "outbound failed", result: InboundSyncResult{OutboundFailed: 1}, changed: true},
		{name: "outbound unconfirmed", result: InboundSyncResult{OutboundUnconfirmed: 1}, changed: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			syncer := &fakeInboundSyncer{outcomes: []syncTestOutcome{{result: test.result}}}
			notifications := &fakeNotificationSender{}
			publisher := &fakeSyncPublisher{}
			coordinator, err := NewSyncCoordinator(syncer, notifications, publisher)
			if err != nil {
				t.Fatal(err)
			}
			report := coordinator.runCycle(context.Background())
			if report.DurableChange != test.changed {
				t.Fatalf("durable change = %t", report.DurableChange)
			}
			if got := len(publisher.publications); got != boolCount(test.changed) {
				t.Fatalf("publication count = %d", got)
			}
			if test.changed && publisher.publications[0].attention != "" {
				t.Fatalf("attention = %q", publisher.publications[0].attention)
			}
			if len(notifications.receivedCalls) != 0 || len(notifications.summaryCalls) != 0 {
				t.Fatalf("notification calls = %#v / %#v", notifications.receivedCalls, notifications.summaryCalls)
			}
		})
	}
}

func TestSyncCoordinatorRunUsesSyncErrorOnlyForRetryAndResetsAfterSuccess(t *testing.T) {
	syncErr := errors.New("synchronization failed")
	syncer := &fakeInboundSyncer{outcomes: []syncTestOutcome{
		{err: syncErr},
		{err: syncErr},
		{},
	}}
	notifications := &fakeNotificationSender{
		receivedErrors: []error{errors.New("must not affect scheduling")},
		summaryError:   errors.New("must not affect scheduling"),
	}
	publisher := &fakeSyncPublisher{}
	coordinator, err := NewSyncCoordinator(syncer, notifications, publisher)
	if err != nil {
		t.Fatal(err)
	}
	var delays []time.Duration
	coordinator.wait = func(_ context.Context, delay time.Duration) bool {
		delays = append(delays, delay)
		return len(delays) < 3
	}
	var reports []SyncReport
	coordinator.Run(context.Background(), 0, func(report SyncReport) {
		reports = append(reports, report)
	})
	if want := []time.Duration{15 * time.Second, 30 * time.Second, 2 * time.Second}; !reflect.DeepEqual(delays, want) {
		t.Fatalf("delays = %v, want %v", delays, want)
	}
	if syncer.calls != 3 || len(reports) != 3 {
		t.Fatalf("sync calls = %d, reports = %d", syncer.calls, len(reports))
	}

	// A notification error is observable but a successful synchronization still uses the normal interval.
	syncer = &fakeInboundSyncer{outcomes: []syncTestOutcome{{result: InboundSyncResult{
		Persisted: 1, receivedSMS: []receivedSMSNotification{{Sender: "10086", Body: "bounded"}},
	}}}}
	coordinator, err = NewSyncCoordinator(syncer, notifications, publisher)
	if err != nil {
		t.Fatal(err)
	}
	coordinator.wait = func(_ context.Context, delay time.Duration) bool {
		if delay != 3*time.Second {
			t.Fatalf("delay after notification error = %s", delay)
		}
		return false
	}
	coordinator.Run(context.Background(), 3*time.Second, nil)
}

func TestSyncRetryDelayBacksOffAndCaps(t *testing.T) {
	delay := time.Duration(0)
	want := []time.Duration{15 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 5 * time.Minute, 5 * time.Minute}
	for index, expected := range want {
		delay = nextSyncRetryDelay(delay, 2*time.Second)
		if delay != expected {
			t.Fatalf("delay %d = %s, want %s", index, delay, expected)
		}
	}
	if got := nextSyncRetryDelay(0, 10*time.Second); got != 40*time.Second {
		t.Fatalf("long interval retry = %s", got)
	}
	if got := nextSyncRetryDelay(0, 2*time.Minute); got != 5*time.Minute {
		t.Fatalf("capped initial retry = %s", got)
	}
}

func TestSyncCoordinatorRunStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	syncer := &fakeInboundSyncer{
		outcomes: []syncTestOutcome{{}},
		inspect: func(context.Context) {
			cancel()
		},
	}
	coordinator, err := NewSyncCoordinator(syncer, &fakeNotificationSender{}, &fakeSyncPublisher{})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		coordinator.Run(ctx, time.Hour, nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("coordinator did not stop after cancellation")
	}
}

func assertDeadlineNear(t *testing.T, ctx context.Context, expected time.Duration) {
	t.Helper()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("context has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= expected-time.Second || remaining > expected {
		t.Fatalf("deadline remaining = %s, expected near %s", remaining, expected)
	}
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}
