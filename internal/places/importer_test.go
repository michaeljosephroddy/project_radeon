package places

import "testing"

func TestParseGeoNamesCity(t *testing.T) {
	row := []string{
		"3128760",
		"Barcelona",
		"Barcelona",
		"Barca,Barcelona",
		"41.38879",
		"2.15899",
		"P",
		"PPLA",
		"ES",
		"",
		"56",
		"B",
		"",
		"",
		"1621537",
		"",
		"10",
		"Europe/Madrid",
	}

	city, err := parseGeoNamesCity(row)
	if err != nil {
		t.Fatalf("parseGeoNamesCity: %v", err)
	}
	if city.SourceID != "3128760" || city.Name != "Barcelona" || city.CountryCode != "ES" || city.Admin1Code != "56" || city.Population != 1621537 || city.Timezone != "Europe/Madrid" {
		t.Fatalf("city = %#v", city)
	}
	if city.Latitude != 41.38879 || city.Longitude != 2.15899 {
		t.Fatalf("coordinates = %f,%f", city.Latitude, city.Longitude)
	}
	if len(city.AlternateNames) != 2 {
		t.Fatalf("alternate names = %#v", city.AlternateNames)
	}
}

func TestBuildSearchTextDedupesParts(t *testing.T) {
	got := buildSearchText("Barcelona", "Barcelona", "Catalonia", "Spain", "ES")
	want := "Barcelona Catalonia Spain ES"
	if got != want {
		t.Fatalf("search text = %q, want %q", got, want)
	}
}
