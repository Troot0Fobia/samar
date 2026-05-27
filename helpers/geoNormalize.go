package helpers

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"unicode"

	"Troot0Fobia/samar/initializers"
	"Troot0Fobia/samar/models"

	"gorm.io/gorm"
)

func geoSimilarityThreshold() float64 {
	if v := os.Getenv("GEO_SIMILARITY_THRESHOLD"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 && f <= 1 {
			return f
		}
	}
	return 0.92
}

// phoneticSoundMap maps Latin characters to phonetically equivalent Cyrillic sounds
// and vice versa, enabling matching between transliterated and native names.
var phoneticSoundMap = map[rune][]string{
	// Latin → possible Cyrillic equivalents
	'p': {"п"},
	'h': {"х", "г", "ґ"},
	'c': {"ц", "с", "к"},
	'x': {"х"},
	'w': {"в", "у"},
	'y': {"и", "й", "ы", "ю", "я"},
	'j': {"ж", "й", "є"},
	'i': {"и", "і", "ї"},
	'e': {"е", "э", "є"},
	'a': {"а"},
	'o': {"о"},
	'u': {"у"},
	's': {"с", "ш"},
	'k': {"к"},
	't': {"т"},
	'm': {"м"},
	'n': {"н"},
	'r': {"р"},
	'l': {"л"},
	'd': {"д"},
	'g': {"г", "ґ"},
	'b': {"б"},
	'v': {"в"},
	'f': {"ф"},
	'z': {"з", "ж"},
	'q': {"к"},
}

// phoneticMatch checks if two keys are phonetically similar by comparing
// each character's sound equivalents. Returns true if enough characters
// have phonetic overlap (useful for matching translit vs native names).
func phoneticMatch(a, b string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	if a == b {
		return true
	}

	// Build phonetic sets for each string
	aPhonetic := buildPhoneticSet(a)
	bPhonetic := buildPhoneticSet(b)

	// Count overlapping phonetic sounds
	matches := 0
	for sound := range aPhonetic {
		if bPhonetic[sound] {
			matches++
		}
	}

	minLen := min(len(aPhonetic), len(bPhonetic))
	if minLen == 0 {
		return false
	}

	// Require at least 70% phonetic overlap
	return float64(matches)/float64(minLen) >= 0.7
}

// buildPhoneticSet creates a set of all phonetic sounds represented by a string
func buildPhoneticSet(s string) map[string]bool {
	set := make(map[string]bool)
	for _, ch := range strings.ToLower(s) {
		if ch >= 'a' && ch <= 'z' {
			// Latin char — add its Cyrillic phonetic equivalents
			if equivalents, ok := phoneticSoundMap[ch]; ok {
				for _, eq := range equivalents {
					set[eq] = true
				}
			}
			set[string(ch)] = true // also add the original
		} else if (ch >= 'а' && ch <= 'я') || ch == 'ґ' || ch == 'є' || ch == 'ї' || ch == 'і' {
			// Cyrillic char — add it directly
			set[string(ch)] = true
		}
	}
	return set
}

// levenshteinSimilarity returns a similarity score (0..1) based on Levenshtein distance
func levenshteinSimilarity(s, t string) float64 {
	if s == t {
		return 1.0
	}
	ls, lt := len(s), len(t)
	if ls == 0 || lt == 0 {
		return 0.0
	}

	// Build distance matrix
	matrix := make([][]int, ls+1)
	for i := range matrix {
		matrix[i] = make([]int, lt+1)
		matrix[i][0] = i
	}
	for j := 0; j <= lt; j++ {
		matrix[0][j] = j
	}

	for i := 1; i <= ls; i++ {
		for j := 1; j <= lt; j++ {
			cost := 1
			if s[i-1] == t[j-1] {
				cost = 0
			}
			matrix[i][j] = min3(
				matrix[i-1][j]+1,
				matrix[i][j-1]+1,
				matrix[i-1][j-1]+cost,
			)
		}
	}

	dist := matrix[ls][lt]
	maxLen := max(ls, lt)
	return 1.0 - float64(dist)/float64(maxLen)
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

// fuzzyMatch combines Jaro-Winkler, Levenshtein, and phonetic matching
// to determine if two strings represent the same place name.
func fuzzyMatch(a, b string, threshold float64) bool {
	if a == b {
		return true
	}
	if a == "" || b == "" {
		return false
	}

	// Primary: Jaro-Winkler
	jw := jaroWinkler(a, b)
	if jw >= threshold {
		return true
	}

	// Secondary: phonetic match
	if phoneticMatch(a, b) {
		return true
	}

	// Tertiary: Levenshtein (only when Jaro-Winkler is close to threshold)
	if jw >= threshold*0.9 {
		lev := levenshteinSimilarity(a, b)
		if lev >= threshold {
			return true
		}
	}

	return false
}

// jaroWinkler computes the Jaro-Winkler similarity between two strings.
func jaroWinkler(s, t string) float64 {
	if s == t {
		return 1.0
	}
	ls, lt := len(s), len(t)
	if ls == 0 || lt == 0 {
		return 0.0
	}
	matchDist := max(max(ls, lt)/2-1, 0)

	sMatches := make([]bool, ls)
	tMatches := make([]bool, lt)
	matches := 0
	transpositions := 0

	for i := 0; i < ls; i++ {
		start := max(0, i-matchDist)
		end := min(i+matchDist+1, lt)
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

	// Winkler prefix bonus
	prefix := 0
	for i := 0; i < min(4, min(ls, lt)); i++ {
		if s[i] == t[i] {
			prefix++
		} else {
			break
		}
	}
	return jaro + float64(prefix)*0.1*(1-jaro)
}


// JaroWinkler returns the Jaro-Winkler similarity between two strings (0..1).
// Exported for use in migration commands.
func JaroWinkler(s, t string) float64 {
	return jaroWinkler(s, t)
}

// GetOrCreateRegion finds or creates a region by key (with fuzzy matching).
// Parameters are the canonical country/region names and their Russian equivalents.
func GetOrCreateRegion(country, countryRus, region, regionRus string) (models.Region, error) {
	key := NormalizeToKey(region)
	if key == "" {
		key = "unknown"
		if region == "" {
			region = "Unknown"
		}
		if regionRus == "" {
			regionRus = "Неизвестно"
		}
	}

	// 1. Find or create the country
	var countryRec models.Country
	err := initializers.DB.Where("name = ?", country).First(&countryRec).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return models.Region{}, err
		}
		countryRec = models.Country{Name: country, Name_rus: countryRus}
		if createErr := initializers.DB.Create(&countryRec).Error; createErr != nil {
			// Race condition: another goroutine created the country, re-fetch
			if strings.Contains(createErr.Error(), "UNIQUE constraint failed") {
				if refetchErr := initializers.DB.Where("name = ?", country).First(&countryRec).Error; refetchErr != nil {
					return models.Region{}, refetchErr
				}
			} else {
				return models.Region{}, createErr
			}
		}
	}

	// 2. Look up region by exact key within country
	var existing models.Region
	err = initializers.DB.
		Where("key = ? AND country_id = ?", key, countryRec.ID).
		First(&existing).Error
	if err == nil {
		// Update Russian name if missing
		if existing.Name_rus == "" && regionRus != "" {
			initializers.DB.Model(&existing).Update("name_rus", regionRus)
			existing.Name_rus = regionRus
		}
		return existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return models.Region{}, err
	}

	// 3. Fuzzy match against all existing regions in this country
	threshold := geoSimilarityThreshold()
	var candidates []models.Region
	initializers.DB.Where("country_id = ?", countryRec.ID).Find(&candidates)

	bestSim := 0.0
	var bestMatch *models.Region
	for i := range candidates {
		sim := jaroWinkler(key, candidates[i].Key)
		if sim > bestSim {
			bestSim = sim
			bestMatch = &candidates[i]
		}
	}
	if bestMatch != nil && bestSim >= threshold {
		return *bestMatch, nil
	}

	// 4. Create new region
	if regionRus == "" {
		regionRus = reverseTranslit(key)
	}
	newRegion := models.Region{
		Key:       key,
		Name:      region,
		Name_rus:  regionRus,
		CountryID: countryRec.ID,
	}
	if err := initializers.DB.Create(&newRegion).Error; err != nil {
		// Race condition: another goroutine created the same region
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			var retry models.Region
			if retryErr := initializers.DB.Where("key = ? AND country_id = ?", key, countryRec.ID).First(&retry).Error; retryErr == nil {
				return retry, nil
			}
		}
		return models.Region{}, err
	}
	return newRegion, nil
}

// toTranslitName converts any name (Cyrillic or Latin) to a Title-case
// transliterated form suitable for display as the canonical name.
// e.g. "київ" → "Kyiv", "sofiyivska borschagivka" → "Sofiyivska Borschagivka"
func toTranslitName(name string) string {
	if name == "" {
		return ""
	}
	var buf strings.Builder
	for _, r := range strings.ToLower(name) {
		if lat, ok := cyrillicToLatin[r]; ok {
			buf.WriteString(lat)
		} else if r <= unicode.MaxASCII {
			buf.WriteRune(r)
		}
		// drop non-ASCII non-Cyrillic characters (like apostrophes)
	}
	s := buf.String()
	// Title-case each word
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == '-' || r == '_'
	})
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}

// toCyrillicName normalizes a Russian/Cyrillic name to Title-case display form.
// Capitalizes the first letter of each word separated by spaces or hyphens,
// e.g. "ивано-франковск" → "Ивано-Франковск", "белая церковь" → "Белая Церковь".
func toCyrillicName(name string) string {
	if name == "" {
		return ""
	}
	s := strings.ToLower(strings.TrimSpace(name))
	if s == "" {
		return ""
	}
	runes := []rune(s)
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

// buildCityRus derives the best available Russian/Cyrillic display name for a city.
// Priority: (1) explicit Cyrillic cityNameRus, (2) Cyrillic cityName, (3) keyToRussian lookup.
// If the result is still Latin (unknown city), it is left as-is; callers may check hasCyrillic.
func buildCityRus(cityName, cityNameRus, key string) string {
	if cityNameRus != "" && hasCyrillic(cityNameRus) {
		return toCyrillicName(cityNameRus)
	}
	if hasCyrillic(cityName) {
		return toCyrillicName(cityName)
	}
	return reverseTranslit(key)
}

// GetOrCreateCity finds or creates a city record within a region.
// cityName can be in any language — it will be normalized.
// cityNameRus should be the Russian/Cyrillic display name.
//
// Resulting fields:
//   - Key: "translit_lowercase" — unique slug (scoped by region_id in DB)
//   - Name: "Translit Title" — canonical English-like name
//   - Name_rus: "Русское название" — Cyrillic display name
func GetOrCreateCity(cityName, cityNameRus string, regionID uint) (models.City, error) {
	if cityName == "" || strings.EqualFold(cityName, "Unknown") {
		return models.City{}, errors.New("unknown city name")
	}

	key := NormalizeToKey(cityName)
	if key == "" {
		return models.City{}, errors.New("empty city key")
	}

	var existing models.City
	err := initializers.DB.
		Where("key = ? AND region_id = ?", key, regionID).
		First(&existing).Error
	if err == nil {
		// Update name_rus if the stored one is Latin (i.e. reverseTranslit fallback)
		// and we now have a proper Cyrillic translation.
		if !hasCyrillic(existing.Name_rus) {
			newRus := buildCityRus(cityName, cityNameRus, key)
			if hasCyrillic(newRus) {
				initializers.DB.Model(&existing).Update("name_rus", newRus)
				existing.Name_rus = newRus
			}
		}
		return existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return models.City{}, err
	}

	// Exact key match only — fuzzy matching is intentionally disabled for cities.
	// Cities like "Poltava" and "Poltavka" score >0.95 JaroWinkler despite being
	// distinct places; safe deduplication is handled via the variantTable instead.

	// Build canonical Name (translit, Title-case)
	displayName := toTranslitName(cityName)
	if displayName == "" {
		displayName = ToDisplayName(key)
	}

	// Build Name_rus (Cyrillic, Title-case).
	cityNameRus = buildCityRus(cityName, cityNameRus, key)

	newCity := models.City{
		Key:      key,
		Name:     displayName,
		Name_rus: cityNameRus,
		RegionID: regionID,
	}
	if err := initializers.DB.Create(&newCity).Error; err != nil {
		// Race condition: another goroutine created the same city
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			var retry models.City
			if retryErr := initializers.DB.Where("key = ? AND region_id = ?", key, regionID).First(&retry).Error; retryErr == nil {
				return retry, nil
			}
		}
		return models.City{}, err
	}
	return newCity, nil
}

// hasCyrillic checks if a string contains Cyrillic characters
func hasCyrillic(s string) bool {
	for _, r := range s {
		if (r >= 'а' && r <= 'я') || r == 'ё' || r == 'ґ' || r == 'є' || r == 'ї' || r == 'і' ||
			(r >= 'А' && r <= 'Я') || r == 'Ё' {
			return true
		}
	}
	return false
}
