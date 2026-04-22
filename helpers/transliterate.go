package helpers

import (
	"regexp"
	"strings"
	"unicode"
)

// normalizeApostrophes replaces the Ukrainian typographic apostrophe (U+2019) with
// the plain ASCII apostrophe so variantTable lookups work regardless of input encoding.
func normalizeApostrophes(s string) string {
	return strings.ReplaceAll(s, "’", "'")
}

// variantTable maps known name variants (including Ukrainian oblast adjective forms)
// to their canonical key form.
var variantTable = map[string]string{
	// Ukrainian oblasts — both adjective and noun forms
	"kyivska":           "kyiv",
	"kyivs'ka":         "kyiv",
	"kievskaya":        "kyiv",
	"kyiv city":        "kyiv",
	"kyiv oblast":      "kyiv",
	"kiev":             "kyiv",
	"kyyiv":            "kyiv",
	"kharkivska":       "kharkiv",
	"kharkivs'ka":      "kharkiv",
	"kharkovskaya":     "kharkiv",
	"kharkiv oblast":   "kharkiv",
	"kharkov":          "kharkiv",
	"odeska":           "odesa",
	"odes'ka":          "odesa",
	"odesskaya":        "odesa",
	"odessa":           "odesa",
	"odesa oblast":     "odesa",
	"dnipropetrovska":  "dnipropetrovsk",
	"dnipropetrovs'ka": "dnipropetrovsk",
	"dnepropetrovskaya": "dnipropetrovsk",
	"dnipro":           "dnipropetrovsk",
	"zaporizka":        "zaporizhzhia",
	"zaporizhs'ka":     "zaporizhzhia",
	"zaporozhskaya":    "zaporizhzhia",
	"zaporizhia":       "zaporizhzhia",
	"zaporizhzhya":     "zaporizhzhia",
	"zaporizhzha":      "zaporizhzhia",
	"lvivska":          "lviv",
	"lvivs'ka":         "lviv",
	"lvovskaya":        "lviv",
	"lviv oblast":      "lviv",
	"lvov":             "lviv",
	"donetska":         "donetsk",
	"donets'ka":        "donetsk",
	"donetskaya":       "donetsk",
	"donetsk oblast":   "donetsk",
	"luhanska":         "luhansk",
	"luhans'ka":        "luhansk",
	"luganskaya":       "luhansk",
	"lugansk":          "luhansk",
	"luhansk oblast":   "luhansk",
	"poltavska":        "poltava",
	"poltavs'ka":       "poltava",
	"poltavskaya":      "poltava",
	"poltava oblast":   "poltava",
	"vinnytska":        "vinnytsia",
	"vinnytsianska":    "vinnytsia",
	"vinnyts'ka":       "vinnytsia",
	"vinnitskaya":      "vinnytsia",
	"vinnytsia oblast": "vinnytsia",
	"vinnitsa":         "vinnytsia",
	"zhytomyrska":      "zhytomyr",
	"zhytomyrs'ka":     "zhytomyr",
	"zhytomyrskaya":    "zhytomyr",
	"zhytomyr oblast":  "zhytomyr",
	"cherkaska":        "cherkasy",
	"cherkas'ka":       "cherkasy",
	"cherkasskaya":     "cherkasy",
	"cherkasy oblast":  "cherkasy",
	"chernihivska":     "chernihiv",
	"chernihivs'ka":    "chernihiv",
	"chernigovskaya":   "chernihiv",
	"chernihiv oblast": "chernihiv",
	"chernigov":        "chernihiv",
	"chernivetska":     "chernivtsi",
	"chernivetss'ka":   "chernivtsi",
	"chernovetskaya":   "chernivtsi",
	"chernivtsi oblast": "chernivtsi",
	"chernovtsy":       "chernivtsi",
	"kirovohradska":    "kirovohrad",
	"kirovohrads'ka":   "kirovohrad",
	"kirovograd":       "kirovohrad",
	"khmelnytska":      "khmelnytskyi",
	"khmelnyts'ka":     "khmelnytskyi",
	"khmelnitskaya":    "khmelnytskyi",
	"khmelnytskyi oblast": "khmelnytskyi",
	"khmelnitsky":      "khmelnytskyi",
	"rivnenska":        "rivne",
	"rivnens'ka":       "rivne",
	"rovenskaya":       "rivne",
	"rivne oblast":     "rivne",
	"rovno":            "rivne",
	"sumska":           "sumy",
	"sums'ka":          "sumy",
	"sumskaya":         "sumy",
	"sumy oblast":      "sumy",
	"ternopilska":      "ternopil",
	"ternopils'ka":     "ternopil",
	"ternopolskaya":    "ternopil",
	"ternopil oblast":  "ternopil",
	"zakarpatska":      "zakarpattia",
	"zakarpats'ka":     "zakarpattia",
	"zakarpatskaya":    "zakarpattia",
	"zakarpattia oblast": "zakarpattia",
	"transkarpatia":    "zakarpattia",
	"ivano-frankivska": "ivano-frankivsk",
	"ivano-frankivs'ka": "ivano-frankivsk",
	"ivano-frankovskaya": "ivano-frankivsk",
	"ivano-frankivsk oblast": "ivano-frankivsk",
	"ivano-frankovsk":  "ivano-frankivsk",
	"mykolaivska":      "mykolaiv",
	"mykolaivs'ka":     "mykolaiv",
	"nikolaevskaya":    "mykolaiv",
	"mykolaiv oblast":  "mykolaiv",
	"nikolaev":         "mykolaiv",
	"khersonska":       "kherson",
	"khersons'ka":      "kherson",
	"khersonskaya":     "kherson",
	"kherson oblast":   "kherson",
	"volynska":         "volyn",
	"volyns'ka":        "volyn",
	"volynskaya":       "volyn",
	"volyn oblast":     "volyn",
	"volyn'":           "volyn",
}

// keyToRussian maps canonical keys to Russian display names.
var keyToRussian = map[string]string{
	"kyiv":             "Киев",
	"kharkiv":          "Харьков",
	"odesa":            "Одесса",
	"dnipropetrovsk":   "Днепропетровск",
	"zaporizhzhia":     "Запорожье",
	"lviv":             "Львов",
	"donetsk":          "Донецк",
	"luhansk":          "Луганск",
	"poltava":          "Полтава",
	"vinnytsia":        "Винница",
	"zhytomyr":         "Житомир",
	"cherkasy":         "Черкассы",
	"chernihiv":        "Чернигов",
	"chernivtsi":       "Черновцы",
	"kirovohrad":       "Кировоград",
	"khmelnytskyi":     "Хмельницкий",
	"rivne":            "Ровно",
	"sumy":             "Сумы",
	"ternopil":         "Тернополь",
	"zakarpattia":      "Закарпатье",
	"ivano-frankivsk":  "Ивано-Франковск",
	"mykolaiv":         "Николаев",
	"kherson":          "Херсон",
	"volyn":            "Волынь",
}

// cyrillicToLatin maps Cyrillic characters to their Latin equivalents for key generation.
var cyrillicToLatin = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "h", 'ґ': "g",
	'д': "d", 'е': "e", 'є': "ye", 'ж': "zh", 'з': "z",
	'и': "y", 'і': "i", 'ї': "yi", 'й': "y", 'к': "k",
	'л': "l", 'м': "m", 'н': "n", 'о': "o", 'п': "p",
	'р': "r", 'с': "s", 'т': "t", 'у': "u", 'ф': "f",
	'х': "kh", 'ц': "ts", 'ч': "ch", 'ш': "sh", 'щ': "shch",
	'ь': "", 'ю': "yu", 'я': "ya",
	// Russian-specific
	'э': "e", 'ё': "yo", 'ъ': "", 'ы': "y",
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)
var adminSuffixes = []string{
	" oblast", " region", " city", " raion", " district",
	"ska", "s'ka", "skaya", "ska oblast", "s'ka oblast",
}

// NormalizeToKey converts a region/city name (any language) to a canonical slug key.
// Pipeline: ToLower → variantTable → Cyrillic translit → strip admin suffixes → slug → post-strip variantTable.
func NormalizeToKey(name string) string {
	if name == "" {
		return ""
	}
	s := normalizeApostrophes(strings.ToLower(strings.TrimSpace(name)))

	// Direct variant lookup before any further processing
	if canonical, ok := variantTable[s]; ok {
		return canonical
	}

	// Transliterate Cyrillic
	var buf strings.Builder
	for _, r := range s {
		if lat, ok := cyrillicToLatin[r]; ok {
			buf.WriteString(lat)
		} else if r <= unicode.MaxASCII {
			buf.WriteRune(r)
		}
		// drop non-ASCII non-Cyrillic characters
	}
	s = buf.String()

	// Strip admin suffixes
	for _, suffix := range adminSuffixes {
		if strings.HasSuffix(s, suffix) {
			s = strings.TrimSuffix(s, suffix)
			break
		}
	}
	s = strings.TrimSpace(s)

	// Slug: keep only a-z0-9, collapse separators to '-'
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")

	// Post-slug variant lookup
	if canonical, ok := variantTable[s]; ok {
		return canonical
	}

	return s
}

// ToDisplayName converts a slug key to a Title-Case display name.
// e.g. "ivano-frankivsk" → "Ivano-Frankivsk"
func ToDisplayName(key string) string {
	parts := strings.Split(key, "-")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "-")
}

// reverseTranslit returns a Russian display name for a canonical key, if known.
func reverseTranslit(key string) string {
	if rus, ok := keyToRussian[key]; ok {
		return rus
	}
	return ToDisplayName(key)
}

// russianToUkrainianCities maps Russian city names to Ukrainian ones.
var russianToUkrainianCities = map[string]string{
	"киев":     "київ",
	"харьков":  "харків",
	"одесса":   "одеса",
	"днепр":    "дніпро",
	"днепропетровск": "дніпро",
	"запорожье": "запоріжжя",
	"львов":    "львів",
	"донецк":   "донецьк",
	"луганск":  "луганськ",
	"полтава":  "полтава",
	"винница":  "вінниця",
	"житомир":  "житомир",
	"черкассы": "черкаси",
	"чернигов": "чернігів",
	"черновцы": "чернівці",
	"кировоград": "кропивницький",
	"хмельницкий": "хмельницький",
	"ровно":    "рівне",
	"сумы":     "суми",
	"тернополь": "тернопіль",
	"ужгород":  "ужгород",
	"ивано-франковск": "івано-франківськ",
	"николаев": "миколаїв",
	"херсон":   "херсон",
	"луцк":     "луцьк",
	"белая церковь": "біла церква",
	"бровары":  "бровари",
	"борисполь": "бориспіль",
	"константиновка": "костянтинівка",
	"славянск": "слов'янськ",
	"краматорск": "краматорськ",
	"бердянск": "бердянськ",
	"мелитополь": "мелітополь",
	"евпатория": "євпаторія",
	"ялта":     "ялта",
	"симферополь": "сімферополь",
	"севастополь": "сєвастополь",
	"керчь":    "керч",
	"никополь": "нікополь",
	"павлоград": "павлоград",
	"каменское": "кам'янське",
	"кременчуг": "кременчук",
	"алчевск":  "алчевськ",
	"лисичанск": "лисічанськ",
	"северодонецк": "сєвєродонецьк",
	"горловка": "горлівка",
	"макеевка": "макіївка",
	"енкиево":  "єнакієве",
}

// russianStreetAbbrevs maps Russian street/type abbreviations to Ukrainian ones.
var russianStreetAbbrevs = map[string]string{
	"ул.":      "вул.",
	"улица":    "вулиця",
	"ул":       "вул",
	"пр.":      "просп.",
	"пр-т":     "просп.",
	"пр-кт":    "просп.",
	"проспект": "проспект",
	"пер.":     "пров.",
	"переулок": "провулок",
	"пер":      "пров",
	"пл.":      "пл.",
	"площадь":  "майдан",
	"площа":    "майдан",
	"бул.":     "бул.",
	"бульвар":  "бульвар",
	"ш.":       "шосе",
	"шоссе":    "шосе",
	"наб.":     "наб.",
	"набережная": "набережна",
	"м-н":      "м-н",
	"микрорайон": "мікрорайон",
}

// nominatimReplacement pairs a precompiled Cyrillic word-boundary regexp with its Ukrainian replacement.
// \b doesn't work for Cyrillic in RE2, so we use explicit boundary characters instead.
type nominatimReplacement struct {
	re *regexp.Regexp
	ua string
}

var nominatimCityRules []nominatimReplacement
var nominatimAbbrRules []nominatimReplacement

func init() {
	// Cyrillic boundary: start-of-string or whitespace/punctuation, end-of-string or whitespace/punctuation.
	boundary := func(ru string) *regexp.Regexp {
		pat := `(^|[\s,.()])` + regexp.QuoteMeta(ru) + `($|[\s,.()])`
		return regexp.MustCompile(pat)
	}
	for ru, ua := range russianToUkrainianCities {
		nominatimCityRules = append(nominatimCityRules, nominatimReplacement{boundary(ru), ua})
	}
	for ru, ua := range russianStreetAbbrevs {
		nominatimAbbrRules = append(nominatimAbbrRules, nominatimReplacement{boundary(ru), ua})
	}
}

// PreprocessNominatimQuery converts Russian address terms in a query to Ukrainian
// so that Nominatim can find Ukrainian addresses.
func PreprocessNominatimQuery(query string) string {
	s := strings.ToLower(strings.TrimSpace(query))

	for _, r := range nominatimCityRules {
		s = r.re.ReplaceAllString(s, "${1}"+r.ua+"${2}")
	}
	for _, r := range nominatimAbbrRules {
		s = r.re.ReplaceAllString(s, "${1}"+r.ua+"${2}")
	}

	return s
}
