package app

import (
	"strings"
	"testing"
)

func TestValidPackageName(t *testing.T) {
	for _, packageName := range []string{
		"com.uchi.app", "com.example.my_app2",
		"Com.uchi.app", "com.Uchi.app", "com.uchi.APP",
		"com.example.calculatorKSA1211", "Alekseyt.Lusher",
		"com." + strings.Repeat("a", 251),
	} {
		if !ValidPackageName(packageName) {
			t.Errorf("valid package name rejected: %q", packageName)
		}
	}
	for _, packageName := range []string{
		"", "invalid", "com..app", ".com.app", "com.app.", "com.1app",
		"com.app-name", "com.app/extra", "../com.app", "com.app?x=1",
		" com.app", "com.app ", "com.app\n", "com.приложение",
		"com." + strings.Repeat("a", 252),
	} {
		if ValidPackageName(packageName) {
			t.Errorf("invalid package name accepted: %q", packageName)
		}
	}
}
