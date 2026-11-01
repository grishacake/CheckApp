package applink

import "testing"

func TestPackageName(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"https://play.google.com/store/apps/details?id=com.uchi.app", "com.uchi.app", true},
		{"https://play.google.com/store/apps/details?id=com.uchi.app&hl=ru&gl=US", "com.uchi.app", true},
		{"play.google.com/store/apps/details?id=com.uchi.app", "com.uchi.app", true},
		{"https://PLAY.GOOGLE.COM/store/apps/details?id=com.uchi.app", "com.uchi.app", true},
		{"https://apps.rustore.ru/app/ru.ozon.app.android", "ru.ozon.app.android", true},
		{"https://apps.rustore.ru/app/ru.ozon.app.android/", "ru.ozon.app.android", true},
		{"https://apps.rustore.ru/app/ru.ozon.app.android?utm_source=tg", "ru.ozon.app.android", true},

		{"telegram", "", false},
		{"com.uchi.app", "", false},
		{"https://example.com/store/apps/details?id=com.uchi.app", "", false},
		{"https://play.google.com/store/apps/developer?id=Google+LLC", "", false},
		{"https://play.google.com/store/apps/details", "", false},
		{"https://play.google.com/store/apps/details?id=../../etc", "", false},
		{"https://apps.rustore.ru/app/", "", false},
		{"https://apps.rustore.ru/developer/123", "", false},
	}
	for _, tt := range tests {
		got, ok := PackageName(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("PackageName(%q) = %q, %v; want %q, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}
