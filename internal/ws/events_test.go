package ws

import "testing"

func TestSubscribeUnsubscribeByStableID(t *testing.T) {
	b := NewEventBroadcaster()

	hits := make([]string, 0, 3)
	u1 := b.Subscribe("dev1", func(*DeviceEvent) { hits = append(hits, "l1") })
	u2 := b.Subscribe("dev1", func(*DeviceEvent) { hits = append(hits, "l2") })
	u3 := b.Subscribe("dev1", func(*DeviceEvent) { hits = append(hits, "l3") })

	// Remove middle listener first.
	u2()
	b.Emit(&DeviceEvent{DeviceID: "dev1"})
	if len(hits) != 2 || hits[0] != "l1" || hits[1] != "l3" {
		t.Fatalf("unexpected listeners after removing middle: %#v", hits)
	}

	// Remove first and ensure third can still be removed correctly.
	hits = hits[:0]
	u1()
	u3()
	b.Emit(&DeviceEvent{DeviceID: "dev1"})
	if len(hits) != 0 {
		t.Fatalf("expected no listeners after removing all, got %#v", hits)
	}
}

func TestSubscribeAllUnsubscribeOrderSafe(t *testing.T) {
	b := NewEventBroadcaster()

	hits := make([]string, 0, 3)
	u1 := b.SubscribeAll(func(*DeviceEvent) { hits = append(hits, "a1") })
	u2 := b.SubscribeAll(func(*DeviceEvent) { hits = append(hits, "a2") })
	u3 := b.SubscribeAll(func(*DeviceEvent) { hits = append(hits, "a3") })

	u2()
	b.Emit(&DeviceEvent{DeviceID: "dev1"})
	if len(hits) != 2 || hits[0] != "a1" || hits[1] != "a3" {
		t.Fatalf("unexpected global listeners after removing middle: %#v", hits)
	}

	hits = hits[:0]
	u1()
	u3()
	b.Emit(&DeviceEvent{DeviceID: "dev1"})
	if len(hits) != 0 {
		t.Fatalf("expected no global listeners after removing all, got %#v", hits)
	}
}
