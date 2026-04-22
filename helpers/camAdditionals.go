package helpers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type location struct {
	Country     string  `json:"country"`
	Country_rus string  `json:"country_rus"`
	Region      string  `json:"region"`
	Region_rus  string  `json:"region_rus"`
	City        string  `json:"city"`
	City_rus    string  `json:"city_rus"`
	Latitude    string  `json:"latitude"`
	Longitude   string  `json:"longitude"`
}

// providerResult holds data extracted from a single check-host.net service
type providerResult struct {
	Service   string
	Country   string
	Region    string
	City      string
	Latitude  float64
	Longitude float64
	HasCoords bool
}

// checkHostProviders defines the 5 services on check-host.net in page order
var checkHostProviders = []struct {
	Key  string // HTML id suffix
	Name string
}{
	{"dbip", "DB-IP"},
	{"ipgeolocation", "IPGeolocation.io"},
	{"ip2location", "IP2Location"},
	{"geolite2", "MaxMind GeoIP"},
	{"ipinfoio", "IPInfo.io"},
}

// providerHTMLRegexes holds pre-compiled regexes for each provider to avoid
// re-compiling them on every parseCheckHostHTML call.
var providerHTMLRegexes = func() []struct{ divRe, divRe2, mapRe *regexp.Regexp } {
	out := make([]struct{ divRe, divRe2, mapRe *regexp.Regexp }, len(checkHostProviders))
	for i, p := range checkHostProviders {
		out[i].divRe = regexp.MustCompile(`(?s)<div\s+id="ip_info-` + p.Key + `"(.*?)</div>\s*<div`)
		out[i].divRe2 = regexp.MustCompile(`(?s)<div\s+id="ip_info-` + p.Key + `"(.*?)(?:<div\s+id="ip_info-|$)`)
		out[i].mapRe = regexp.MustCompile(`map_info\.addMap\("map-` + p.Key + `",\s*([^,\s]+),\s*([^,\s]+),`)
	}
	return out
}()

// stripTagsRe and countryCodeRe are used repeatedly in extractLabelValue/parseCheckHostHTML.
var (
	stripTagsRe    = regexp.MustCompile(`<[^>]*>`)
	countryCodeRe  = regexp.MustCompile(`\s*\([A-Z]{2}\)\s*$`)
)

// parseCheckHostHTML extracts geolocation data from all 5 services on the page
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

// labelValueRegexes caches compiled regexes for extractLabelValue per label.
// The known labels (Country, Region, City) are fixed, so this is effectively static.
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

// extractLabelValue finds the value after a given label in an HTML block
func extractLabelValue(block, label string) string {
	re1, re2, re3 := getLabelRegexes(label)

	if m := re1.FindStringSubmatch(block); len(m) >= 2 {
		val := strings.TrimSpace(stripTagsRe.ReplaceAllString(m[1], ""))
		if val != "" {
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

// consensusLocation determines the most likely location from multiple providers
func consensusLocation(providers []providerResult) (location, bool) {
	// Filter out providers with no region data
	var valid []providerResult
	for _, p := range providers {
		if p.Region != "" {
			valid = append(valid, p)
		}
	}
	if len(valid) == 0 {
		return location{}, false
	}

	// Normalize all region/city names to keys
	type scoredProvider struct {
		providerResult
		RegionKey string
		CityKey   string
	}
	scored := make([]scoredProvider, len(valid))
	for i, p := range valid {
		scored[i] = scoredProvider{
			providerResult: p,
			RegionKey:      NormalizeToKey(p.Region),
			CityKey:        NormalizeToKey(p.City),
		}
	}

	// Cluster by (region_key, city_key) using fuzzy matching
	type cluster struct {
		RegionKey string
		CityKey   string
		Members   []scoredProvider
	}
	var clusters []cluster
	threshold := geoSimilarityThreshold()

	for _, sp := range scored {
		if sp.RegionKey == "" {
			continue
		}
		placed := false
		for ci := range clusters {
			cl := &clusters[ci]
			// Compare region keys
			regionMatch := sp.RegionKey == cl.RegionKey ||
				jaroWinkler(sp.RegionKey, cl.RegionKey) >= threshold ||
				phoneticMatch(sp.RegionKey, cl.RegionKey)
			// Compare city keys (empty city = wildcard match)
			cityMatch := sp.CityKey == "" || cl.CityKey == "" ||
				sp.CityKey == cl.CityKey ||
				jaroWinkler(sp.CityKey, cl.CityKey) >= threshold ||
				phoneticMatch(sp.CityKey, cl.CityKey)
			if regionMatch && cityMatch {
				cl.Members = append(cl.Members, sp)
				placed = true
				break
			}
		}
		if !placed {
			clusters = append(clusters, cluster{
				RegionKey: sp.RegionKey,
				CityKey:   sp.CityKey,
				Members:   []scoredProvider{sp},
			})
		}
	}

	if len(clusters) == 0 {
		return location{}, false
	}

	// Select winning cluster: most members, ties broken by first-in-page order
	bestCluster := &clusters[0]
	for i := range clusters {
		if len(clusters[i].Members) > len(bestCluster.Members) {
			bestCluster = &clusters[i]
		}
		// If equal size, keep the one that appears first (already in page order)
	}

	// Average coordinates from winning cluster
	var sumLat, sumLng float64
	var coordCount int
	var bestProvider *scoredProvider // provider with coords, first in page order
	for i := range bestCluster.Members {
		m := &bestCluster.Members[i]
		if m.HasCoords {
			sumLat += m.Latitude
			sumLng += m.Longitude
			coordCount++
			if bestProvider == nil {
				bestProvider = m
			}
		}
	}

	avgLat := 0.0
	avgLng := 0.0
	if coordCount > 0 {
		avgLat = sumLat / float64(coordCount)
		avgLng = sumLng / float64(coordCount)
	}

	// Pick display names from the first provider with coords in the cluster,
	// otherwise from the first provider
	displayProvider := bestProvider
	if displayProvider == nil {
		displayProvider = &bestCluster.Members[0]
	}

	loc := location{
		Country:     displayProvider.Country,
		Region:      displayProvider.Region,
		City:        displayProvider.City,
		Latitude:    fmt.Sprintf("%.6f", avgLat),
		Longitude:   fmt.Sprintf("%.6f", avgLng),
	}

	// Generate Russian names: preserve Cyrillic if provider already gave it,
	// otherwise fall back to transliteration table.
	cyrillicOrTranslit := func(raw, key string) string {
		if hasCyrillic(raw) {
			return toCyrillicName(raw)
		}
		return reverseTranslit(key)
	}
	if loc.Country != "" {
		loc.Country_rus = cyrillicOrTranslit(loc.Country, NormalizeToKey(loc.Country))
	}
	if loc.Region != "" {
		loc.Region_rus = cyrillicOrTranslit(loc.Region, loc.RegionKey())
	}
	if loc.City != "" {
		loc.City_rus = cyrillicOrTranslit(loc.City, NormalizeToKey(loc.City))
	}

	return loc, true
}

// RegionKey returns the normalized key for the region (used internally)
func (l location) RegionKey() string {
	return NormalizeToKey(l.Region)
}

// getLocationFromIPAPI fetches geolocation from ip-api.com
func getLocationFromIPAPI(ip string) (location, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://ip-api.com/json/%s", ip))
	if err != nil {
		return location{}, err
	}
	defer resp.Body.Close()

	var apiResp struct {
		Status      string  `json:"status"`
		Country     string  `json:"country"`
		RegionName  string  `json:"regionName"`
		City        string  `json:"city"`
		Lat         float64 `json:"lat"`
		Lon         float64 `json:"lon"`
		CountryCode string  `json:"countryCode"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return location{}, err
	}
	if apiResp.Status != "success" {
		return location{}, errors.New("ip-api returned non-success status")
	}

	loc := location{
		Country:     apiResp.Country,
		Country_rus: reverseTranslit(NormalizeToKey(apiResp.Country)),
		Region:      apiResp.RegionName,
		Region_rus:  reverseTranslit(NormalizeToKey(apiResp.RegionName)),
		City:        apiResp.City,
		City_rus:    reverseTranslit(NormalizeToKey(apiResp.City)),
		Latitude:    fmt.Sprintf("%.6f", apiResp.Lat),
		Longitude:   fmt.Sprintf("%.6f", apiResp.Lon),
	}
	return loc, nil
}

// GetLocation determines geographic location of an IP address.
// Primary: check-host.net (via proxy if available, otherwise direct).
// Fallback: ip-api.com JSON API.
func GetLocation(ip string) (location, error) {
	// Try check-host.net (proxy if available, otherwise direct)
	var client *http.Client
	var proxyUrlStr string

	proxy, err := GetProxy()
	if err == nil && proxy.ProxyUrl != "" {
		proxyUrlStr = proxy.ProxyUrl
		proxyUrl, perr := url.Parse(proxyUrlStr)
		if perr == nil {
			tr := &http.Transport{Proxy: http.ProxyURL(proxyUrl)}
			client = &http.Client{Timeout: 15 * time.Second, Transport: tr}
		}
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	resp, err := client.Get(fmt.Sprintf("https://check-host.net/ip-info?host=%s", ip))
	if err == nil {
		htmlBytes, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr == nil && resp.StatusCode == 200 {
			html := string(htmlBytes)
			providers := parseCheckHostHTML(html)
			if loc, ok := consensusLocation(providers); ok {
				// Update proxy usage count on successful parse
				if proxyUrlStr != "" {
					if updErr := UpdateProxyUsageCount(1, proxyUrlStr); updErr != nil {
						LogError("Error updating proxy usage count", "", updErr.Error())
					}
				}
				LogSuccess(fmt.Sprintf("Location for %s resolved via check-host.net: %s, %s", ip, loc.Region, loc.City), "")
				return loc, nil
			}
			// Got a 200 response but parsing failed — still count the proxy as used
			if proxyUrlStr != "" {
				if updErr := UpdateProxyUsageCount(1, proxyUrlStr); updErr != nil {
					LogError("Error updating proxy usage count", "", updErr.Error())
				}
			}
		}
	} else {
		LogError("Error sending request to check-host.net", "", err.Error())
	}

	// Fallback: ip-api.com (direct, no proxy)
	LogSuccess(fmt.Sprintf("Falling back to ip-api.com for %s", ip), "")
	loc, err := getLocationFromIPAPI(ip)
	if err != nil {
		LogError("Error receiving location from ip-api.com", "", err.Error())
		return location{}, errors.New("error with request")
	}

	// Validate that we got meaningful region data (private IPs return empty)
	if loc.Region == "" {
		return location{}, errors.New("no region data for this IP")
	}

	LogSuccess(fmt.Sprintf("Location for %s resolved via ip-api.com: %s, %s", ip, loc.Region, loc.City), "")
	return loc, nil
}

func ParseCoords(coords string) (float64, float64, error) {
	split_coords := strings.Split(coords, ",")
	if len(split_coords) < 2 {
		return 0.0, 0.0, errors.New("invalid coords format: expected 'lat,lng'")
	}

	lat, err := strconv.ParseFloat(strings.Trim(split_coords[0], ", "), 64)
	if err != nil {
		return 0.0, 0.0, err
	}

	lng, err := strconv.ParseFloat(strings.Trim(split_coords[1], ", "), 64)
	if err != nil {
		return 0.0, 0.0, err
	}

	return lat, lng, nil
}

func GetLastPhotoIndex(baseDir, ip, port string) (int, error) {
	camDir := filepath.Join(baseDir, ip)

	if _, err := os.Stat(camDir); os.IsNotExist(err) {
		return 1, nil
	}

	prefix := ip + "_" + port + "_"
	const suffix = ".jpg"
	maxIndex := 0

	err := filepath.WalkDir(camDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
			return nil
		}
		numStr := name[len(prefix) : len(name)-len(suffix)]
		if idx, err := strconv.Atoi(numStr); err == nil && idx > maxIndex {
			maxIndex = idx
		}
		return nil
	})

	if err != nil {
		return 0, err
	}
	return maxIndex + 1, nil
}
