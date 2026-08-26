package provider

import (
	"testing"
)

const (
	testSubMonitorID = "4e49d7a2-4ab5-45e2-b9f8-1d59f505ad45"
	testSubContactID = "0c3c7b07-cecb-43dd-9b76-8516d3b9c771"
)

func TestSubscriptionIDRoundTrips(t *testing.T) {
	id := subscriptionID(testSubMonitorID, testSubContactID)
	if id != testSubMonitorID+"/"+testSubContactID {
		t.Fatalf("unexpected spelling: %q", id)
	}

	monitorID, contactID, err := parseSubscriptionID(id)
	if err != nil {
		t.Fatalf("parsing the id: %v", err)
	}
	if monitorID != testSubMonitorID || contactID != testSubContactID {
		t.Fatalf("expected the pair back, got %q and %q", monitorID, contactID)
	}
}

func TestParseSubscriptionIDRefusesWhatIsNotOne(t *testing.T) {
	for _, id := range []string{
		"",
		testSubMonitorID,
		"/" + testSubContactID,
		testSubMonitorID + "/",
		testSubMonitorID + "/" + testSubContactID + "/extra",
		" / ",
	} {
		if _, _, err := parseSubscriptionID(id); err == nil {
			t.Fatalf("expected %q to be refused", id)
		}
	}
}

func TestSubscriptionPointerMapperPlacesTheOneMember(t *testing.T) {
	mapper := subscriptionPointerMapper("alertTypes", "alert_types")
	got, ok := mapper("/alertTypes")
	if !ok || got.String() != "alert_types" {
		t.Fatalf("expected the set to be named, got %v %v", got, ok)
	}
	if _, ok := mapper("/somethingElse"); ok {
		t.Fatal("the body vocabulary is closed: nothing else maps")
	}

	mapper = subscriptionPointerMapper("frequencies", "frequencies")
	if got, ok := mapper("/frequencies"); !ok || got.String() != "frequencies" {
		t.Fatalf("expected the frequencies to be named, got %v %v", got, ok)
	}
}
