package heatmap

import "strings"

// isUnrecognized reports screen names that carry no usable identity: empty, the SDK fallbacks "App" and
// "Screen", and iOS system windows (UI...WindowController).
func isUnrecognized(screen string) bool {
	switch screen {
	case "", "App", "Screen":
		return true
	}
	return strings.HasPrefix(screen, "UI") && strings.HasSuffix(screen, "WindowController")
}
