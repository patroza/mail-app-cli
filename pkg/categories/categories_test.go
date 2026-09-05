package categories

import "testing"

func TestMembershipAndSenderAreDifferent(t *testing.T) {
	r, err := Parse("membership", "Primary\tfalse\nTransactions\tfalse\nUpdates\ttrue\nPromotions\tfalse\n\n", 72)
	if err != nil || len(r.Categories) != 1 || r.Categories[0] != "Updates" {
		t.Fatalf("%+v %v", r, err)
	}
	r, err = Parse("inspect", "Automatically\ttrue\t✓\nPrimary\ttrue\t\nTransactions\ttrue\t\nUpdates\ttrue\t\nPromotions\ttrue\t\n", 72)
	if err != nil || r.SenderRule != "Automatically" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestRejectIncompleteReport(t *testing.T) {
	for _, input := range []string{"", "Primary\ttrue\n", "Primary\ttrue\nTransactions\tfalse\nUpdates\tunknown\nPromotions\tfalse\n"} {
		if _, err := Parse("membership", input, 1); err == nil {
			t.Fatal("accepted incomplete or invalid report")
		}
	}
}

func TestRejectInjectedCategory(t *testing.T) {
	for _, name := range []string{"", "Primary; do shell script", "All Mail"} {
		if ValidName(name, true) {
			t.Fatal(name)
		}
	}
	if !ValidName("Automatically", true) || ValidName("Automatically", false) {
		t.Fatal("automatic mode validation")
	}
}
