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
		{"https://www.rustore.ru/catalog/app/com.example.calculatorKSA1211", "com.example.calculatorKSA1211", true},
		{"https://www.rustore.ru/catalog/app/ru.ivi.client", "ru.ivi.client", true},
		{"https://www.rustore.ru/catalog/app/ru.ivi.client/?utm_source=tg", "ru.ivi.client", true},
		{"www.rustore.ru/catalog/app/ru.ivi.client", "ru.ivi.client", true},
		{"https://rustore.ru/catalog/app/ru.ivi.client", "ru.ivi.client", true},
		{"https://WWW.RUSTORE.RU/catalog/app/ru.ivi.client", "ru.ivi.client", true},
		{"https://www.rustore.ru/catalog/app/ru.Ivi.client", "ru.Ivi.client", true},
		{"https://www.rustore.ru/catalog/app/ru.ivi.client/extra", "", false},
		{"https://play.google.com/store/apps/details?id=com.first&id=com.second", "", false},
		{"https://apps.rustore.ru/app/com.example.calculatorKSA1211", "com.example.calculatorKSA1211", true},
		{"https://play.google.com/store/apps/details?id=Com.Uchi.APP", "Com.Uchi.APP", true},

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

func TestParseAPKPureLink(t *testing.T) {
	for _, tc := range []struct {
		url, id string
	}{
		{"https://apkpure.com/telegram/org.telegram.messenger", "org.telegram.messenger"},
		{"https://apkpure.com/p/com.example.MixedCase", "com.example.MixedCase"},
		{"apkpure.net/ru/whatsapp-android/com.whatsapp/?utm_source=share", "com.whatsapp"},
		{"https://www.apkpure.com/pt-BR/p/com.example.MixedCase", "com.example.MixedCase"},
		{"https://apkpure.com/telegram/org.telegram.messenger/download", ""},
		{"https://apkpure.com/../com.example.app", ""},
		{"https://apkpure.com:8443/p/com.example.app", ""},
		{"https://apkpure.com.example.org/p/com.example.app", ""},
		{"https://apkpure.com@example.org/p/com.example.app", ""},
	} {
		link, ok := Parse(tc.url)
		if ok != (tc.id != "") || ok && (link.Store != "apkpure" || link.PackageName != tc.id) {
			t.Errorf("Parse(%q) = %+v, %v; want APKPure ID %q", tc.url, link, ok, tc.id)
		}
	}
}
