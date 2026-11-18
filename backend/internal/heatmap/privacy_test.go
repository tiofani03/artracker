package heatmap

import "testing"

func TestIsSensitive(t *testing.T) {
	yes := []string{"PinInputActivity", "android://LoginFragment", "OtpViewController", "PaymentSheet",
		"SecurityQuestion", "CardNumberForm", "SignInViewController", "enter_pin_screen", "OTPView"}
	no := []string{"GiftCardViewController", "HomePageViewController", "android://ShoppingFragment",
		"BasketActivity", "SpinWheelActivity", "TopUpViewController"}
	for _, s := range yes {
		if !IsSensitive(s) {
			t.Errorf("IsSensitive(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if IsSensitive(s) {
			t.Errorf("IsSensitive(%q) = true, want false", s)
		}
	}
}

func TestSkipFunc(t *testing.T) {
	skip := SkipFunc([]string{"BasketActivity"})
	if !skip("BasketActivity") || !skip("PinInputActivity") {
		t.Error("expected excluded and sensitive screens to be skipped")
	}
	if skip("android://ShoppingFragment") {
		t.Error("ShoppingFragment must not be skipped")
	}
	if skip("basketactivity") {
		t.Error("exclusion match is case-sensitive")
	}
}
