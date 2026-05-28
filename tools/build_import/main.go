// build_import — standalone geo-resolver for surveillance camera lists.
//
// Reads a text file with IP:PORT LOGIN:PASS entries, resolves each IP's
// geographic location via check-host.net (with ip-api.com as fallback),
// and writes a JSON file ready for import via the site's /admin/upload_cams.
//
// Usage:
//
//	go run tools/build_import/main.go -in cameras.txt -out result.json -delay 1500
//
// Input formats supported (one entry per line, blank lines / # comments skipped):
//
//	IP:PORT LOGIN:PASS
//	1\tIP:PORT LOGIN:PASS
//	1\tIP:PORT LOGIN:PASS - dahua
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// ─── Output format (matches /admin/upload_cams JSON schema) ──────────────────

type importEntry struct {
	IP          string  `json:"ip"`
	Port        string  `json:"port"`
	Login       string  `json:"login"`
	Password    string  `json:"password"`
	Channels    string  `json:"channels"`
	City        string  `json:"city"`
	City_rus    string  `json:"city_rus"`
	Region      string  `json:"region"`
	Region_rus  string  `json:"region_rus"`
	Country     string  `json:"country"`
	Country_rus string  `json:"country_rus"`
	Lat         float64 `json:"lat"`
	Lng         float64 `json:"lng"`
}

// ─── Input record ─────────────────────────────────────────────────────────────

type inputRecord struct {
	ip    string
	port  string
	login string
	pass  string
}

// ─── Geo types ────────────────────────────────────────────────────────────────

type location struct {
	Country     string
	Country_rus string
	Region      string
	Region_rus  string
	City        string
	City_rus    string
	Latitude    string
	Longitude   string
}

type providerResult struct {
	Service   string
	Country   string
	Region    string
	City      string
	Latitude  float64
	Longitude float64
	HasCoords bool
}

// ─── check-host.net HTML parsing ─────────────────────────────────────────────

var checkHostProviders = []struct {
	Key  string
	Name string
}{
	{"dbip", "DB-IP"},
	{"ipgeolocation", "IPGeolocation.io"},
	{"ip2location", "IP2Location"},
	{"geolite2", "MaxMind GeoIP"},
	{"ipinfoio", "IPInfo.io"},
}

var providerHTMLRegexes = func() []struct{ divRe, divRe2, mapRe *regexp.Regexp } {
	out := make([]struct{ divRe, divRe2, mapRe *regexp.Regexp }, len(checkHostProviders))
	for i, p := range checkHostProviders {
		out[i].divRe = regexp.MustCompile(`(?s)<div\s+id="ip_info-` + p.Key + `"(.*?)</div>\s*<div`)
		out[i].divRe2 = regexp.MustCompile(`(?s)<div\s+id="ip_info-` + p.Key + `"(.*?)(?:<div\s+id="ip_info-|$)`)
		out[i].mapRe = regexp.MustCompile(`map_info\.addMap\("map-` + p.Key + `",\s*([^,\s]+),\s*([^,\s]+),`)
	}
	return out
}()

var (
	stripTagsRe   = regexp.MustCompile(`<[^>]*>`)
	countryCodeRe = regexp.MustCompile(`\s*\([A-Z]{2}\)\s*$`)
)

var labelValueRegexes sync.Map

func getLabelRegexes(label string) (re1, re2, re3 *regexp.Regexp) {
	type trio struct{ r1, r2, r3 *regexp.Regexp }
	q := regexp.QuoteMeta(label)
	val, _ := labelValueRegexes.LoadOrStore(label, &trio{
		r1: regexp.MustCompile(`(?i)<t[hd][^>]*>.*?` + q + `.*?</t[hd]>\s*<t[hd][^>]*>(.*?)</t[hd]>`),
		r2: regexp.MustCompile(`(?i)<b>` + q + `</b>\s*(?:<[^>]*>\s*)*(.*?)(?:<br|</div|</td|</tr|</p)`),
		r3: regexp.MustCompile(`(?i)` + q + `\s*[:)]\s*(.*?)(?:<br|</div|</td|</tr|</p|<t[hd])`),
	})
	t := val.(*trio)
	return t.r1, t.r2, t.r3
}

func extractLabelValue(block, label string) string {
	re1, re2, re3 := getLabelRegexes(label)
	if m := re1.FindStringSubmatch(block); len(m) >= 2 {
		if val := strings.TrimSpace(stripTagsRe.ReplaceAllString(m[1], "")); val != "" {
			return val
		}
	}
	if m := re2.FindStringSubmatch(block); len(m) >= 2 {
		return strings.TrimSpace(stripTagsRe.ReplaceAllString(m[1], ""))
	}
	if m := re3.FindStringSubmatch(block); len(m) >= 2 {
		return strings.TrimSpace(stripTagsRe.ReplaceAllString(m[1], ""))
	}
	return ""
}

func parseCheckHostHTML(html string) []providerResult {
	results := make([]providerResult, 0, len(checkHostProviders))
	for i, p := range checkHostProviders {
		pr := providerResult{Service: p.Key}
		rx := providerHTMLRegexes[i]

		divMatch := rx.divRe.FindStringSubmatch(html)
		if len(divMatch) < 2 {
			divMatch = rx.divRe2.FindStringSubmatch(html)
		}
		if len(divMatch) < 2 {
			results = append(results, pr)
			continue
		}
		block := divMatch[1]

		pr.Country = strings.TrimSpace(countryCodeRe.ReplaceAllString(extractLabelValue(block, "Country"), ""))
		pr.Region = strings.TrimSpace(extractLabelValue(block, "Region"))
		pr.City = strings.TrimSpace(extractLabelValue(block, "City"))

		if mapMatch := rx.mapRe.FindStringSubmatch(html); len(mapMatch) >= 3 {
			latStr := strings.TrimSpace(mapMatch[1])
			lngStr := strings.TrimSpace(mapMatch[2])
			if latStr != "null" && lngStr != "null" {
				if lat, err := strconv.ParseFloat(latStr, 64); err == nil {
					if lng, err2 := strconv.ParseFloat(lngStr, 64); err2 == nil {
						pr.Latitude = lat
						pr.Longitude = lng
						pr.HasCoords = true
					}
				}
			}
		}
		results = append(results, pr)
	}
	return results
}

// ─── Jaro-Winkler similarity ─────────────────────────────────────────────────

func jaroWinkler(s, t string) float64 {
	if s == t {
		return 1.0
	}
	ls, lt := len(s), len(t)
	if ls == 0 || lt == 0 {
		return 0.0
	}
	matchDist := ls
	if lt > matchDist {
		matchDist = lt
	}
	matchDist = matchDist/2 - 1
	if matchDist < 0 {
		matchDist = 0
	}

	sMatches := make([]bool, ls)
	tMatches := make([]bool, lt)
	matches, transpositions := 0, 0

	for i := 0; i < ls; i++ {
		start := i - matchDist
		if start < 0 {
			start = 0
		}
		end := i + matchDist + 1
		if end > lt {
			end = lt
		}
		for j := start; j < end; j++ {
			if tMatches[j] || s[i] != t[j] {
				continue
			}
			sMatches[i] = true
			tMatches[j] = true
			matches++
			break
		}
	}
	if matches == 0 {
		return 0.0
	}

	k := 0
	for i := 0; i < ls; i++ {
		if !sMatches[i] {
			continue
		}
		for !tMatches[k] {
			k++
		}
		if s[i] != t[k] {
			transpositions++
		}
		k++
	}

	jaro := (float64(matches)/float64(ls) +
		float64(matches)/float64(lt) +
		float64(matches-transpositions/2)/float64(matches)) / 3.0

	maxPfx := ls
	if lt < maxPfx {
		maxPfx = lt
	}
	if maxPfx > 4 {
		maxPfx = 4
	}
	prefix := 0
	for i := 0; i < maxPfx; i++ {
		if s[i] == t[i] {
			prefix++
		} else {
			break
		}
	}
	return jaro + float64(prefix)*0.1*(1-jaro)
}

// ─── Consensus algorithm ──────────────────────────────────────────────────────

const geoThreshold = 0.88

func normKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func consensusLocation(providers []providerResult) (location, bool) {
	var valid []providerResult
	for _, p := range providers {
		if p.Region != "" {
			valid = append(valid, p)
		}
	}
	if len(valid) == 0 {
		return location{}, false
	}

	type scored struct {
		providerResult
		rk, ck string
	}
	sc := make([]scored, len(valid))
	for i, p := range valid {
		sc[i] = scored{providerResult: p, rk: normKey(p.Region), ck: normKey(p.City)}
	}

	type cluster struct {
		rk, ck  string
		members []scored
	}
	var clusters []cluster

	for _, sp := range sc {
		placed := false
		for ci := range clusters {
			cl := &clusters[ci]
			regionMatch := sp.rk == cl.rk || jaroWinkler(sp.rk, cl.rk) >= geoThreshold
			cityMatch := sp.ck == "" || cl.ck == "" || sp.ck == cl.ck || jaroWinkler(sp.ck, cl.ck) >= geoThreshold
			if regionMatch && cityMatch {
				cl.members = append(cl.members, sp)
				placed = true
				break
			}
		}
		if !placed {
			clusters = append(clusters, cluster{rk: sp.rk, ck: sp.ck, members: []scored{sp}})
		}
	}
	if len(clusters) == 0 {
		return location{}, false
	}

	best := &clusters[0]
	for i := range clusters {
		if len(clusters[i].members) > len(best.members) {
			best = &clusters[i]
		}
	}

	var sumLat, sumLng float64
	var coordCount int
	var display *scored
	for i := range best.members {
		m := &best.members[i]
		if m.HasCoords {
			sumLat += m.Latitude
			sumLng += m.Longitude
			coordCount++
			if display == nil {
				display = m
			}
		}
	}
	if display == nil {
		display = &best.members[0]
	}

	avgLat, avgLng := 0.0, 0.0
	if coordCount > 0 {
		avgLat = sumLat / float64(coordCount)
		avgLng = sumLng / float64(coordCount)
	}

	loc := location{
		Country:   display.Country,
		Region:    display.Region,
		City:      display.City,
		Latitude:  fmt.Sprintf("%.6f", avgLat),
		Longitude: fmt.Sprintf("%.6f", avgLng),
	}
	loc.Country_rus = toRus(loc.Country)
	loc.Region_rus = toRus(loc.Region)
	loc.City_rus = toRus(loc.City)

	return loc, true
}

// ─── ip-api.com fallback ──────────────────────────────────────────────────────

func getLocationFromIPAPI(ip string) (location, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://ip-api.com/json/%s?fields=status,country,regionName,city,lat,lon", ip))
	if err != nil {
		return location{}, err
	}
	defer resp.Body.Close()

	var r struct {
		Status     string  `json:"status"`
		Country    string  `json:"country"`
		RegionName string  `json:"regionName"`
		City       string  `json:"city"`
		Lat        float64 `json:"lat"`
		Lon        float64 `json:"lon"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return location{}, err
	}
	if r.Status != "success" {
		return location{}, fmt.Errorf("ip-api: status=%s", r.Status)
	}

	return location{
		Country:     r.Country,
		Country_rus: toRus(r.Country),
		Region:      r.RegionName,
		Region_rus:  toRus(r.RegionName),
		City:        r.City,
		City_rus:    toRus(r.City),
		Latitude:    fmt.Sprintf("%.6f", r.Lat),
		Longitude:   fmt.Sprintf("%.6f", r.Lon),
	}, nil
}

// ─── Main geo resolver ────────────────────────────────────────────────────────

func resolveGeo(ip string) (location, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(fmt.Sprintf("https://check-host.net/ip-info?host=%s", ip))
	if err == nil {
		htmlBytes, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr == nil && resp.StatusCode == 200 {
			providers := parseCheckHostHTML(string(htmlBytes))
			if loc, ok := consensusLocation(providers); ok {
				return loc, nil
			}
		}
	}

	// Fallback to ip-api.com
	loc, err := getLocationFromIPAPI(ip)
	if err != nil {
		return location{}, err
	}
	if loc.Region == "" {
		return location{}, fmt.Errorf("no region data for %s (possibly private IP)", ip)
	}
	return loc, nil
}

// ─── Russian name generation ─────────────────────────────────────────────────

// countryRus provides known Russian equivalents for common countries.
// For other countries the algorithm handles transliteration.
var countryRus = map[string]string{
	"ukraine":        "Украина",
	"russia":         "Россия",
	"russian federation": "Россия",
	"belarus":        "Беларусь",
	"moldova":        "Молдова",
	"republic of moldova": "Молдова",
	"kazakhstan":     "Казахстан",
	"uzbekistan":     "Узбекистан",
	"georgia":        "Грузия",
	"armenia":        "Армения",
	"azerbaijan":     "Азербайджан",
	"kyrgyzstan":     "Кыргызстан",
	"tajikistan":     "Таджикистан",
	"turkmenistan":   "Туркменистан",
	"romania":        "Румыния",
	"bulgaria":       "Болгария",
	"poland":         "Польша",
	"hungary":        "Венгрия",
	"slovakia":       "Словакия",
	"czech republic": "Чехия",
	"czechia":        "Чехия",
	"germany":        "Германия",
	"france":         "Франция",
	"italy":          "Италия",
	"spain":          "Испания",
	"turkey":         "Турция",
}

// hasCyrillic reports whether s contains at least one Cyrillic character.
func hasCyrillic(s string) bool {
	for _, r := range s {
		if (r >= 'а' && r <= 'я') || r == 'ё' || r == 'ґ' || r == 'є' || r == 'ї' || r == 'і' ||
			(r >= 'А' && r <= 'Я') || r == 'Ё' {
			return true
		}
	}
	return false
}

// toCyrillicTitle title-cases a Cyrillic string on word boundaries.
func toCyrillicTitle(s string) string {
	if s == "" {
		return ""
	}
	runes := []rune(strings.ToLower(strings.TrimSpace(s)))
	capitalizeNext := true
	for i, r := range runes {
		if r == ' ' || r == '-' {
			capitalizeNext = true
		} else if capitalizeNext {
			runes[i] = unicode.ToUpper(r)
			capitalizeNext = false
		}
	}
	return string(runes)
}

// uaGeoRus maps lowercase English names (as returned by geo APIs in Ukrainian
// DSTU transliteration) to their Russian equivalents. Keys use spaces, not dashes,
// because toRus passes raw lowercased names before any slug normalisation.
var uaGeoRus = map[string]string{
	// Oblasts — full form with "oblast" suffix
	"kyiv oblast":                  "Киевская Область",
	"kyivska oblast":               "Киевская Область",
	"kyiv city":                    "Киев",
	"kharkiv oblast":               "Харьковская Область",
	"kharkivska oblast":            "Харьковская Область",
	"kharkivs'ka oblast'":          "Харьковская Область",
	"kharkivs`ka oblast`":          "Харьковская Область",
	"dnipropetrovsk oblast":        "Днепропетровская Область",
	"dnipropetrovska oblast":       "Днепропетровская Область",
	"zaporizhzhia oblast":          "Запорожская Область",
	"zaporizka oblast":             "Запорожская Область",
	"donetsk oblast":               "Донецкая Область",
	"donetska oblast":              "Донецкая Область",
	"luhansk oblast":               "Луганская Область",
	"luhanska oblast":              "Луганская Область",
	"lviv oblast":                  "Львовская Область",
	"lviv region":                  "Львовская Область",
	"lvivska oblast":               "Львовская Область",
	"odesa oblast":                 "Одесская Область",
	"odeska oblast":                "Одесская Область",
	"mykolaiv oblast":              "Николаевская Область",
	"mykolayiv oblast":             "Николаевская Область",
	"mykolaivska oblast":           "Николаевская Область",
	"vinnytsia oblast":             "Винницкая Область",
	"vinnytska oblast":             "Винницкая Область",
	"zhytomyr oblast":              "Житомирская Область",
	"zhytomyrska oblast":           "Житомирская Область",
	"chernihiv oblast":             "Черниговская Область",
	"chernihivska oblast":          "Черниговская Область",
	"chernivtsi oblast":            "Черновицкая Область",
	"chernivetska oblast":          "Черновицкая Область",
	"khmelnytskyi oblast":          "Хмельницкая Область",
	"khmelnytska oblast":           "Хмельницкая Область",
	"rivne oblast":                 "Ровенская Область",
	"rivnenska oblast":             "Ровенская Область",
	"ternopil oblast":              "Тернопольская Область",
	"ternopilska oblast":           "Тернопольская Область",
	"zakarpattia oblast":           "Закарпатская Область",
	"zakarpatska oblast":           "Закарпатская Область",
	"ivano-frankivsk oblast":       "Ивано-Франковская Область",
	"ivano-frankivska oblast":      "Ивано-Франковская Область",
	"ivano-frankivsk region":       "Ивано-Франковская Область",
	"poltava oblast":               "Полтавская Область",
	"poltavska oblast":             "Полтавская Область",
	"sumy oblast":                  "Сумская Область",
	"sumska oblast":                "Сумская Область",
	"kherson oblast":               "Херсонская Область",
	"khersonska oblast":            "Херсонская Область",
	"volyn oblast":                 "Волынская Область",
	"volynska oblast":              "Волынская Область",
	"cherkasy oblast":              "Черкасская Область",
	"cherkaska oblast":             "Черкасская Область",
	"kirovohrad oblast":            "Кировоградская Область",
	"kirovohradska oblast":         "Кировоградская Область",
	"crimea":                       "Крым",
	"autonomous republic of crimea": "Крым",
	"republic of crimea":           "Крым",

	// Oblast adjective-only forms (from check-host.net)
	"kharkivs'ka":   "Харьковская",
	"kyivska":       "Киевская",
	"lvivska":       "Львовская",
	"poltavska":     "Полтавская",
	"sumska":        "Сумская",

	// Cities that differ significantly from their Ukrainian transliteration
	"kyiv":          "Киев",
	"kharkiv":       "Харьков",
	"lviv":          "Львов",
	"dnipro":        "Днепр",
	"odesa":         "Одесса",
	"zaporizhzhia":  "Запорожье",
	"mykolaiv":      "Николаев",
	"mykolayiv":     "Николаев",
	"vinnytsia":     "Винница",
	"chernihiv":     "Чернигов",
	"chernivtsi":    "Черновцы",
	"khmelnytskyi":  "Хмельницкий",
	"rivne":         "Ровно",
	"ternopil":      "Тернополь",
	"ivano-frankivsk": "Ивано-Франковск",
	"kryvyi rih":    "Кривой Рог",
	"kropyvnytskyi": "Кропивницкий",
	"bila tserkva":  "Белая Церковь",
	"kamianets-podilskyi": "Каменец-Подольский",
	"kherson":       "Херсон",
	"zhytomyr":      "Житомир",
	"poltava":       "Полтава",
	"sumy":          "Сумы",
	"lutsk":         "Луцк",
	"uzhhorod":      "Ужгород",
	"shepetivka":    "Шепетовка",
	"radekhiv":      "Радехов",
	"kramatorsk":    "Краматорск",
	"mariupol":      "Мариуполь",
	"donetsk":       "Донецк",
	"luhansk":       "Луганск",
	"horlivka":      "Горловка",
	"makiivka":      "Макеевка",
	"simferopol":    "Симферополь",
	"sevastopol":    "Севастополь",
	"kerch":         "Керчь",
	"yevpatoriia":   "Евпатория",
	"yevpatoriya":   "Евпатория",
	"bakhchysarai":  "Бахчисарай",
	"melitopol":     "Мелитополь",
	"boryspil":      "Бориспиль",
	"irpin":         "Ирпень",
	"drohobych":     "Дрогобич",
	"berdychiv":     "Бердичев",
	"konotop":       "Конотоп",
	"nizhyn":        "Нежин",
	"shostka":       "Шостка",
	"uman":          "Умань",
}

// toRus converts a name (Latin or Cyrillic) to its Russian/Cyrillic equivalent.
// Priority: (1) Cyrillic input → title-case as-is,
//
//	(2) known country lookup,
//	(3) known Ukrainian geo lookup,
//	(4) algorithmic reverse transliteration.
func toRus(name string) string {
	if name == "" {
		return ""
	}
	if hasCyrillic(name) {
		return toCyrillicTitle(name)
	}
	key := strings.ToLower(strings.TrimSpace(name))
	if rus, ok := countryRus[key]; ok {
		return rus
	}
	if rus, ok := uaGeoRus[key]; ok {
		return rus
	}
	return reverseTranslitKey(key)
}

// reverseTranslitKey converts a Latin key (lowercase, hyphens as word separators)
// to a Cyrillic Title-case string using DSTU 9112:2021 reverse mapping.
// Longest-match digraph order: shch(4) → sch(3) → two-char → one-char.
func reverseTranslitKey(key string) string {
	if key == "" {
		return ""
	}
	s := strings.NewReplacer("-", " ", "_", " ").Replace(key)

	// Replace known administrative words so unknown regions still read correctly
	// (e.g. "foobar oblast" → "Фообар область" instead of "Фообар Област").
	wordSubs := map[string]string{
		"oblast": "область",
		"region": "область",
		"raion":  "район",
		"city":   "город",
	}
	ws := strings.Fields(s)
	for idx, w := range ws {
		if sub, ok := wordSubs[w]; ok {
			ws[idx] = sub
		}
	}
	s = strings.Join(ws, " ")

	runes := []rune(s)
	var buf strings.Builder
	i := 0
	for i < len(runes) {
		r := runes[i]
		if r == ' ' {
			buf.WriteRune(' ')
			i++
			continue
		}
		rem := string(runes[i:])
		switch {
		case strings.HasPrefix(rem, "shch"):
			buf.WriteString("щ"); i += 4
		case strings.HasPrefix(rem, "sch"):
			buf.WriteString("щ"); i += 3
		case strings.HasPrefix(rem, "zh"):
			buf.WriteString("ж"); i += 2
		case strings.HasPrefix(rem, "sh"):
			buf.WriteString("ш"); i += 2
		case strings.HasPrefix(rem, "kh"):
			buf.WriteString("х"); i += 2
		case strings.HasPrefix(rem, "ch"):
			buf.WriteString("ч"); i += 2
		case strings.HasPrefix(rem, "ts"):
			buf.WriteString("ц"); i += 2
		case strings.HasPrefix(rem, "yu"):
			buf.WriteString("ю"); i += 2
		case strings.HasPrefix(rem, "ya"):
			buf.WriteString("я"); i += 2
		case strings.HasPrefix(rem, "ye"):
			buf.WriteString("е"); i += 2
		case strings.HasPrefix(rem, "yi"):
			buf.WriteString("и"); i += 2
		case strings.HasPrefix(rem, "yo"):
			buf.WriteString("ё"); i += 2
		default:
			switch r {
			case 'a':
				buf.WriteString("а")
			case 'b':
				buf.WriteString("б")
			case 'v':
				buf.WriteString("в")
			case 'h':
				buf.WriteString("г") // Ukrainian г → h (DSTU)
			case 'g':
				buf.WriteString("г")
			case 'd':
				buf.WriteString("д")
			case 'e':
				buf.WriteString("е")
			case 'z':
				buf.WriteString("з")
			case 'i':
				buf.WriteString("и")
			case 'y':
				buf.WriteString("и")
			case 'k':
				buf.WriteString("к")
			case 'l':
				buf.WriteString("л")
			case 'm':
				buf.WriteString("м")
			case 'n':
				buf.WriteString("н")
			case 'o':
				buf.WriteString("о")
			case 'p':
				buf.WriteString("п")
			case 'r':
				buf.WriteString("р")
			case 's':
				buf.WriteString("с")
			case 't':
				buf.WriteString("т")
			case 'u':
				buf.WriteString("у")
			case 'f':
				buf.WriteString("ф")
			default:
				buf.WriteRune(r)
			}
			i++
		}
	}

	// Title-case on word boundaries
	rr := []rune(buf.String())
	capitalizeNext := true
	for ii, c := range rr {
		if c == ' ' || c == '-' {
			capitalizeNext = true
			continue
		}
		if capitalizeNext {
			rr[ii] = unicode.ToUpper(c)
			capitalizeNext = false
		}
	}
	return string(rr)
}

// ─── Input parsing ────────────────────────────────────────────────────────────

func parseInputFile(path string) ([]inputRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var records []inputRecord
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Strip leading "INDEX\t"
		if idx := strings.IndexByte(line, '\t'); idx != -1 {
			line = strings.TrimSpace(line[idx+1:])
		}

		// Split into "IP:PORT" and "LOGIN:PASS [- TYPE]"
		spaceIdx := strings.IndexByte(line, ' ')
		if spaceIdx == -1 {
			log.Printf("line %d: skip (no space separator): %q", lineNum, line)
			continue
		}
		ipPort := strings.TrimSpace(line[:spaceIdx])
		rest := strings.TrimSpace(line[spaceIdx+1:])

		// Strip optional " - TYPE" suffix (e.g. " - dahua", " - hikvision")
		if i := strings.LastIndex(rest, " - "); i != -1 {
			rest = strings.TrimSpace(rest[:i])
		}

		// Parse IP:PORT — use last colon to handle any future IPv6 brackets
		colonIdx := strings.LastIndex(ipPort, ":")
		if colonIdx == -1 {
			log.Printf("line %d: skip (no port in %q)", lineNum, ipPort)
			continue
		}
		ip := ipPort[:colonIdx]
		port := ipPort[colonIdx+1:]

		// Parse LOGIN:PASS — first colon separates login from (possibly colon-containing) password
		colonIdx2 := strings.Index(rest, ":")
		if colonIdx2 == -1 {
			log.Printf("line %d: skip (no password separator in %q)", lineNum, rest)
			continue
		}
		login := rest[:colonIdx2]
		pass := rest[colonIdx2+1:]

		if ip == "" || port == "" {
			log.Printf("line %d: skip (empty ip or port)", lineNum)
			continue
		}

		records = append(records, inputRecord{ip: ip, port: port, login: login, pass: pass})
	}
	return records, scanner.Err()
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	inFile := flag.String("in", "", "input file with IP:PORT LOGIN:PASS entries (required)")
	outFile := flag.String("out", "import_output.json", "output JSON file path")
	delayMs := flag.Int("delay", 1500, "delay between check-host.net requests in milliseconds")
	flag.Parse()

	if *inFile == "" {
		fmt.Fprintln(os.Stderr, "Usage: build_import -in <file> [-out <file>] [-delay <ms>]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Input formats (one per line, blank / # lines skipped):")
		fmt.Fprintln(os.Stderr, "  IP:PORT LOGIN:PASS")
		fmt.Fprintln(os.Stderr, "  1\\tIP:PORT LOGIN:PASS")
		fmt.Fprintln(os.Stderr, "  1\\tIP:PORT LOGIN:PASS - dahua")
		os.Exit(1)
	}

	records, err := parseInputFile(*inFile)
	if err != nil {
		log.Fatalf("read input: %v", err)
	}
	if len(records) == 0 {
		log.Fatal("no valid records in input file")
	}
	fmt.Fprintf(os.Stderr, "Loaded %d entries from %s\n", len(records), *inFile)
	fmt.Fprintf(os.Stderr, "Delay between requests: %d ms\n\n", *delayMs)

	delay := time.Duration(*delayMs) * time.Millisecond
	var entries []importEntry
	resolved, failed := 0, 0

	for i, rec := range records {
		if i > 0 {
			time.Sleep(delay)
		}

		fmt.Fprintf(os.Stderr, "[%d/%d] %s:%s ... ", i+1, len(records), rec.ip, rec.port)

		loc, err := resolveGeo(rec.ip)
		if err != nil {
			fmt.Fprintf(os.Stderr, "FAILED: %v\n", err)
			failed++
			// Still include the entry with empty geo so user can fill in manually
			entries = append(entries, importEntry{
				IP:       rec.ip,
				Port:     rec.port,
				Login:    rec.login,
				Password: rec.pass,
			})
			continue
		}

		lat, _ := strconv.ParseFloat(loc.Latitude, 64)
		lng, _ := strconv.ParseFloat(loc.Longitude, 64)
		fmt.Fprintf(os.Stderr, "%s, %s\n", loc.City, loc.Country)
		resolved++

		entries = append(entries, importEntry{
			IP:          rec.ip,
			Port:        rec.port,
			Login:       rec.login,
			Password:    rec.pass,
			Country:     loc.Country,
			Country_rus: loc.Country_rus,
			Region:      loc.Region,
			Region_rus:  loc.Region_rus,
			City:        loc.City,
			City_rus:    loc.City_rus,
			Lat:         lat,
			Lng:         lng,
		})
	}

	out, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		log.Fatalf("marshal JSON: %v", err)
	}
	if err := os.WriteFile(*outFile, out, 0644); err != nil {
		log.Fatalf("write output: %v", err)
	}

	fmt.Fprintf(os.Stderr, "\n%d resolved, %d failed → %s\n", resolved, failed, *outFile)
}
