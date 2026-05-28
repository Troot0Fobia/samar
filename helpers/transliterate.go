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

	// ── City-level variants ────────────────────────────────────────────────
	// These arise from IP-geolocation services returning Russian, mixed, or
	// alternative transliterations of Ukrainian city names.

	// Kyiv
	"kyev":             "kyiv", // mis-translit: е instead of и
	"kiyv":             "kyiv", // transposition variant
	"kyiv-city":        "kyiv",

	// Ivano-Frankivsk
	"yvano-frankivsk":  "ivano-frankivsk", // Y/I prefix confusion
	"yvano-frankovsk":  "ivano-frankivsk", // Y/I + Russian suffix

	// Dnipro / Dnipropetrovsk
	"dnepr":            "dnipropetrovsk", // Russian 'Днепр'
	"dnipr":            "dnipropetrovsk", // truncated
	"dnepropetrovsk":   "dnipropetrovsk", // Russian full name

	// Zaporizhzhia
	"zaporozhe":        "zaporizhzhia", // Russian 'Запорожье'
	"zaporizhzhe":      "zaporizhzhia", // near-miss variant
	"zaporozhye":       "zaporizhzhia", // another Russian form

	// Ternopil
	"ternopol":         "ternopil", // Russian 'Тернополь' → slug
	"ternopil'":        "ternopil", // apostrophe soft-sign form

	// Khmelnytskyi
	"khmelnytskyy":     "khmelnytskyi", // double-y ending
	"khmelnytskyj":     "khmelnytskyi", // j-ending
	"khmelnytsky":      "khmelnytskyi", // short form
	"khmelnitskiy":     "khmelnytskyi", // Russian translit

	// Kropyvnytskyi (formerly Kirovograd)
	"kropyvnytskyy":    "kropyvnytskyi", // double-y ending
	"kropyvnytsky":     "kropyvnytskyi", // short form
	"kropyvnitsky":     "kropyvnytskyi", // v-form variant


	// Vinnytsia
	"vynnytsa":         "vinnytsia", // Ukrainian short form
	"vinnytsa":         "vinnytsia", // intermediate

	// Chernihiv
	"chernyhov":        "chernihiv", // Cyr Russian "Чернигов" via DSTU и→y
	"chernihovv":       "chernihiv", // typo double-v

	// Cherkasy — Cyr Russian "Черкассы" → DSTU и→y → "cherkassy"
	"cherkassy":        "cherkasy",

	// Kremenchuk
	"kremenchuh":       "kremenchuk", // h/k confusion (check-host variant)
	"kremenchug":       "kremenchuk", // Russian 'г'

	// Boryspil
	"boryspol":         "boryspil", // Russian -ol suffix
	"borispol":         "boryspil", // Russian Boris- prefix

	// Uzhhorod
	"uzhgorod":         "uzhhorod", // Russian single-h form
	"uzhhorod-city":    "uzhhorod",

	// Mykolaiv
	// Latin: "Nikolaev" → slug "nikolaev" → already in table above
	// Cyrillic Russian "Николаев" → DSTU и→y → "nykolaev" (different!)
	// Cyrillic Ukrainian "Миколаїв" → ї→yi → "mykolayiv"
	"mykolayiv":        "mykolaiv",
	"mykolayiv-city":   "mykolaiv",
	"nykolaev":         "mykolaiv", // Cyr Russian "Николаев" via DSTU и→y

	// Sloviansk (Donetsk region)
	"slavyansk":        "sloviansk", // Russian 'Славянск'
	"slavyans-k":       "sloviansk", // apostrophe-converted form

	// Bila Tserkva
	"belaya-tserkov":   "bila-tserkva", // Russian 'Белая Церковь'
	"bila-tserkva-city": "bila-tserkva",

	// Kamianske (formerly Dniprodzerzhynsk / Kamenskoye)
	"kamenskoe":        "kamianske", // Russian 'Каменское'
	"kamenskoye":       "kamianske",
	"dniprodzerzhynsk": "kamianske",

	// Kamianets-Podilskyi
	"kamyanets-podolskyy":  "kamianets-podilskyi", // Russian form
	"kamyanets-podilskyi":  "kamianets-podilskyi", // intermediate translit
	"kamenets-podolsky":    "kamianets-podilskyi", // another Russian form
	"kamieniec-podolski":   "kamianets-podilskyi", // Polish form
	"kamiianets-podilskyi": "kamianets-podilskyi", // double-i variant

	// Nikopol
	"nykopol":          "nikopol", // y/i variant

	// Kryvyi Rih
	"kryvoy-roh":       "kryvyi-rih", // Russian 'Кривой Рог'
	"krivoy-rog":       "kryvyi-rih",
	"krivoj-rog":       "kryvyi-rih",
	"kryvyy-rih":       "kryvyi-rih", // double-y

	// Zhytomyr
	"zhitomir":         "zhytomyr", // Russian form

	// Pereiaslav
	"pereyaslav":                "pereiaslav",
	"pereyaslav-khmelnytskyi":   "pereiaslav",
	"pereiaslav-khmelnytskyi":   "pereiaslav",

	// Lutsk
	"luck":             "lutsk", // Polish/old form

	// Chernomorsk (Odesa region)
	"chernomors-k":     "chornomorsk", // apostrophe → -s-k artefact

	// Crimea — Cyr Russian input via DSTU и→y produces "y" variants
	// "Симферополь" → symferopol (≠ canonical simferopol from Latin input)
	// "Севастополь" → sevastopol (е→e, no и — already correct ✓)
	"symferopol":       "simferopol",
	"symferopol'":      "simferopol",

	// Kirovohrad — Cyr Russian "Кировоград" → и→y → "kyrovohrad"
	"kyrovohrad":       "kirovohrad",

	// Apostrophe-containing names — caught before apostrophe stripping in NormalizeToKey.
	// NB: hyphen-artifact forms (l-viv, khmel-nyts-kyy, etc.) are no longer needed because
	// apostrophes are now stripped before the slug step, so they never become hyphens.
	"l'viv":            "lviv",
	"khmel'nyts'kyy":   "khmelnytskyi",
	"slovians'k":       "sloviansk",
}

// keyToRussian maps canonical keys to Russian display names for cases where the
// algorithmic back-transliteration (reverseTranslitKeyToRussian) produces a result
// that is incorrect or differs noticeably from the accepted Russian form.
// All other cities are handled by the algorithm automatically.
var keyToRussian = map[string]string{
	// Oblast centres / regions — Russian name differs from Ukrainian transliteration
	"kyiv":             "Киев",
	"kharkiv":          "Харьков",
	"odesa":            "Одесса",
	"dnipropetrovsk":   "Днепропетровск",
	"zaporizhzhia":     "Запорожье",
	"lviv":             "Львов",
	"vinnytsia":        "Винница",
	"chernihiv":        "Чернигов",
	"chernivtsi":       "Черновцы",
	"khmelnytskyi":     "Хмельницкий",
	"rivne":            "Ровно",
	"ternopil":         "Тернополь",
	"zakarpattia":      "Закарпатье",
	"ivano-frankivsk":  "Ивано-Франковск",
	"mykolaiv":         "Николаев",
	"cherkasy":         "Черкассы",
	"kropyvnytskyi":    "Кропивницкий",

	// Major cities — Russian name is meaningfully different
	"kryvyi-rih":       "Кривой Рог",
	"bila-tserkva":     "Белая Церковь",
	"kremenchuk":       "Кременчуг",
	"kremenchuh":       "Кременчуг",
	"irpin":            "Ирпень",
	"sloviansk":        "Славянск",
	"kamianske":        "Каменское",
	"kamianets-podilskyi": "Каменец-Подольский",
	"mohyliv-podilskyi": "Могилев-Подольский",
	"bilhorod-dnistrovskyi": "Белгород-Днестровский",
	"volodymyr-volynskyi": "Владимир-Волынский",
	"pereiaslav":       "Переяслав",
	"boryspil":         "Бориспиль",

	// Soft-sign endings — algorithm cannot recover ь from a key (ь → "" in translit)
	"simferopol":       "Симферополь",
	"sevastopol":       "Севастополь",
	"kerch":            "Керчь",
	"melitopol":        "Мелитополь",
	"nikopol":          "Никополь",
	"korosten":         "Коростень",

	// Crimea cities
	"yevpatoriia":      "Евпатория",
	"yevpatoriya":      "Евпатория",
	"bakhchysarai":     "Бахчисарай",
	"feodosia":         "Феодосия",
	"feodosiya":        "Феодосия",

	// Donetsk/Luhansk region — Ukrainian/Russian root vowel differs
	"horlivka":         "Горловка",
	"makiivka":         "Макеевка",
	"avdiivka":         "Авдеевка",
	"kostiantynivka":   "Константиновка",
	"sievierodonetsk":  "Северодонецк",
	"rubizhne":         "Рубежное",

	// Oblast compound keys (NormalizeToKey of "Foo Oblast" → "foo-oblast")
	"kyiv-oblast":                      "Киевская Область",
	"kyivska-oblast":                   "Киевская Область",
	"kyiv-city":                        "Киев",
	"kharkiv-oblast":                   "Харьковская Область",
	"kharkivska-oblast":                "Харьковская Область",
	"kharkivska-oblast-ua":             "Харьковская Область",
	"dnipropetrovsk-oblast":            "Днепропетровская Область",
	"dnipropetrovska-oblast":           "Днепропетровская Область",
	"zaporizhzhia-oblast":              "Запорожская Область",
	"zaporizka-oblast":                 "Запорожская Область",
	"donetsk-oblast":                   "Донецкая Область",
	"donetska-oblast":                  "Донецкая Область",
	"luhansk-oblast":                   "Луганская Область",
	"luhanska-oblast":                  "Луганская Область",
	"lviv-oblast":                      "Львовская Область",
	"lvivska-oblast":                   "Львовская Область",
	"odesa-oblast":                     "Одесская Область",
	"odeska-oblast":                    "Одесская Область",
	"mykolaiv-oblast":                  "Николаевская Область",
	"mykolayiv-oblast":                 "Николаевская Область",
	"mykolaivska-oblast":               "Николаевская Область",
	"vinnytsia-oblast":                 "Винницкая Область",
	"vinnytska-oblast":                 "Винницкая Область",
	"zhytomyr-oblast":                  "Житомирская Область",
	"zhytomyrska-oblast":               "Житомирская Область",
	"chernihiv-oblast":                 "Черниговская Область",
	"chernihivska-oblast":              "Черниговская Область",
	"chernivtsi-oblast":                "Черновицкая Область",
	"chernivetska-oblast":              "Черновицкая Область",
	"khmelnytskyi-oblast":              "Хмельницкая Область",
	"khmelnytska-oblast":               "Хмельницкая Область",
	"rivne-oblast":                     "Ровенская Область",
	"rivnenska-oblast":                 "Ровенская Область",
	"ternopil-oblast":                  "Тернопольская Область",
	"ternopilska-oblast":               "Тернопольская Область",
	"zakarpattia-oblast":               "Закарпатская Область",
	"zakarpatska-oblast":               "Закарпатская Область",
	"ivano-frankivsk-oblast":           "Ивано-Франковская Область",
	"ivano-frankivska-oblast":          "Ивано-Франковская Область",
	"poltava-oblast":                   "Полтавская Область",
	"poltavska-oblast":                 "Полтавская Область",
	"sumy-oblast":                      "Сумская Область",
	"sumska-oblast":                    "Сумская Область",
	"kherson-oblast":                   "Херсонская Область",
	"khersonska-oblast":                "Херсонская Область",
	"volyn-oblast":                     "Волынская Область",
	"volynska-oblast":                  "Волынская Область",
	"cherkasy-oblast":                  "Черкасская Область",
	"cherkaska-oblast":                 "Черкасская Область",
	"kirovohrad-oblast":                "Кировоградская Область",
	"kirovohradska-oblast":             "Кировоградская Область",
	"crimea":                           "Крым",
	"autonomous-republic-of-crimea":    "Крым",
	"republic-of-crimea":               "Крым",
	"ar-crimea":                        "Крым",

	// Additional cities missing from the table
	"dnipro":       "Днепр",
	"mykolayiv":    "Николаев",
	"kherson":      "Херсон",
	"zhytomyr":     "Житомир",
	"poltava":      "Полтава",
	"sumy":         "Сумы",
	"lutsk":        "Луцк",
	"uzhhorod":     "Ужгород",
	"shepetivka":   "Шепетовка",
	"radekhiv":     "Радехов",
	"kramatorsk":   "Краматорск",
	"mariupol":     "Мариуполь",
	"donetsk":      "Донецк",
	"luhansk":      "Луганск",
	"drohobych":    "Дрогобич",
	"berdychiv":    "Бердичев",
	"konotop":      "Конотоп",
	"nizhyn":       "Нежин",
	"shostka":      "Шостка",
	"uman":         "Умань",
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
// adminSuffixes are stripped from region/city name slugs after transliteration.
// Only compound forms ("ska oblast") are kept — bare "ska" / "s'ka" / "skaya" are
// intentionally omitted because they false-match real city names like "Dolynska",
// "Monastyryska", "Rava-Ruska". Oblast adjective forms without " oblast" suffix are
// handled explicitly in variantTable (kyivska→kyiv, kharkivska→kharkiv, etc.).
var adminSuffixes = []string{
	" oblast", " region", " city", " raion", " district",
	"ska oblast", "s'ka oblast", "skaya oblast",
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

	// Strip apostrophes BEFORE admin-suffix check so that trailing apostrophes
	// (e.g. "Kharkivs'ka Oblast'" → "kharkivska oblast") don't prevent suffix matching,
	// and so that apostrophes in city names (e.g. "Dykan'ka") never become hyphens.
	s = strings.ReplaceAll(s, "'", "")

	// Strip admin suffixes (compound patterns only — see adminSuffixes comment).
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

// reverseTranslitKeyToRussian algorithmically converts a canonical Latin slug key
// back to a Cyrillic Russian display name using digraph-aware back-transliteration.
// It applies the inverse of the Ukrainian DSTU 9112:2021 standard, which is used
// to generate the key in the first place:
//   г → h, х → kh, ж → zh, ш → sh, щ → shch, ч → ch, ц → ts
//   ю → yu, я → ya, є → ye, ї → yi, и → y, і → i
//
// Digraphs are checked longest-first to avoid ambiguity.
// The result is Title-cased on space/hyphen word boundaries.
func reverseTranslitKeyToRussian(key string) string {
	if key == "" {
		return ""
	}
	// Hyphens are word separators in slug keys
	s := strings.NewReplacer("-", " ", "_", " ").Replace(key)

	// Word-level substitutions for known administrative terms that don't
	// transliterate well from Ukrainian DSTU (e.g. "oblast" → "область").
	wordSubs := map[string]string{
		"oblast": "область",
		"region": "область",
		"raion":  "район",
		"city":   "город",
	}
	words := strings.Fields(s)
	for idx, w := range words {
		if sub, ok := wordSubs[w]; ok {
			words[idx] = sub
		}
	}
	s = strings.Join(words, " ")

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
		// 4-char digraph first
		case strings.HasPrefix(rem, "shch"):
			buf.WriteString("щ")
			i += 4
		// 3-char digraphs
		case strings.HasPrefix(rem, "sch"):
			buf.WriteString("щ")
			i += 3
		// 2-char digraphs
		case strings.HasPrefix(rem, "zh"):
			buf.WriteString("ж")
			i += 2
		case strings.HasPrefix(rem, "sh"):
			buf.WriteString("ш")
			i += 2
		case strings.HasPrefix(rem, "kh"):
			buf.WriteString("х")
			i += 2
		case strings.HasPrefix(rem, "ch"):
			buf.WriteString("ч")
			i += 2
		case strings.HasPrefix(rem, "ts"):
			buf.WriteString("ц")
			i += 2
		case strings.HasPrefix(rem, "yu"):
			buf.WriteString("ю")
			i += 2
		case strings.HasPrefix(rem, "ya"):
			buf.WriteString("я")
			i += 2
		case strings.HasPrefix(rem, "ye"):
			buf.WriteString("е")
			i += 2
		case strings.HasPrefix(rem, "yi"):
			buf.WriteString("и")
			i += 2
		case strings.HasPrefix(rem, "yo"):
			buf.WriteString("ё")
			i += 2
		default:
			switch r {
			case 'a':
				buf.WriteString("а")
			case 'b':
				buf.WriteString("б")
			case 'v':
				buf.WriteString("в")
			case 'h':
				buf.WriteString("г") // Ukrainian г → h (DSTU 9112:2021)
			case 'g':
				buf.WriteString("г") // foreign/pre-reform г → g
			case 'd':
				buf.WriteString("д")
			case 'e':
				buf.WriteString("е")
			case 'z':
				buf.WriteString("з")
			case 'i':
				buf.WriteString("и") // Ukrainian і → i
			case 'y':
				buf.WriteString("и") // Ukrainian и → y
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

	// Title-case each word (space/hyphen boundaries)
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

// reverseTranslit returns a Russian display name for a canonical key.
// Priority: (1) keyToRussian explicit lookup; (2) algorithmic back-transliteration;
// (3) Latin ToDisplayName as last resort.
func reverseTranslit(key string) string {
	if rus, ok := keyToRussian[key]; ok {
		return rus
	}
	if result := reverseTranslitKeyToRussian(key); result != "" {
		return result
	}
	return ToDisplayName(key)
}

// ReverseTranslit is the exported version of reverseTranslit.
func ReverseTranslit(key string) string { return reverseTranslit(key) }

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
