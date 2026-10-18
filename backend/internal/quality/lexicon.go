package quality

import (
	"bufio"
	"embed"
	"fmt"
	"slices"
	"strings"
)

//go:embed words/*.txt
var wordFiles embed.FS

// builtIn holds each language's word lists, read once from words/.
var builtIn = map[Language]map[RuleID][]string{}

func init() {
	for _, lang := range Languages {
		lists, err := readWords("words/" + string(lang) + ".txt")
		if err != nil {
			panic(err)
		}
		builtIn[lang] = lists
	}
}

func readWords(name string) (map[RuleID][]string, error) {
	f, err := wordFiles.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[RuleID][]string{}
	var rule RuleID
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			rule = RuleID(line[1 : len(line)-1])
			if !slices.Contains(WordRules, rule) {
				return nil, fmt.Errorf("%s: %q is not a rule that takes words", name, rule)
			}
		case rule == "":
			return nil, fmt.Errorf("%s: %q comes before any rule", name, line)
		default:
			out[rule] = append(out[rule], line)
		}
	}
	return out, scan.Err()
}

// language is the language whose rules apply; one the rules do not know reads as English.
func language(l Language) Language {
	if l == German {
		return German
	}
	return English
}

// grammar is the words a language's sentence rules look for.
type grammar struct {
	// modals say what shall be.
	modals map[string]bool
	// negatives are the phrases that say what shall not be.
	negatives []string
	// beforeNumber are vague only before a number, such as about 5; notBeforeNumber unbounded only without one,
	// as all 3 sensors are bounded.
	beforeNumber, notBeforeNumber map[string]bool
	// openers start a sentence with a condition, of an EARS shape or of none.
	openers []opener
	// thenWords close the condition of an unwanted behaviour.
	thenWords map[string]bool
	// triggers put a condition after the shall, where EARS wants it before.
	triggers                 [][]string
	pronouns, demonstratives map[string]bool
	// auxiliaries after a demonstrative make it the subject, as in This is required.
	auxiliaries map[string]bool
}

// opener is the words a sentence's condition starts with, and the shape they give it; PatternNone fits no
// EARS shape.
type opener struct {
	words   []string
	pattern Pattern
	// needsThen says the clause wants a then before the shall.
	needsThen bool
}

func set(words ...string) map[string]bool {
	out := make(map[string]bool, len(words))
	for _, w := range words {
		out[w] = true
	}
	return out
}

func openers(ears map[string]Pattern, needsThen string, none ...string) []opener {
	var out []opener
	for w, p := range ears {
		out = append(out, opener{words: strings.Fields(w), pattern: p, needsThen: w == needsThen})
	}
	for _, w := range none {
		out = append(out, opener{words: strings.Fields(w), pattern: PatternNone})
	}
	// The longest opener wins, and the order is fixed so a check is the same every time.
	slices.SortFunc(out, func(a, b opener) int {
		if len(a.words) != len(b.words) {
			return len(b.words) - len(a.words)
		}
		return strings.Compare(strings.Join(a.words, " "), strings.Join(b.words, " "))
	})
	return out
}

var grammars = map[Language]*grammar{
	English: {
		modals:          set("shall", "must"),
		negatives:       []string{"shall not", "must not", "shall no longer", "must no longer", "never"},
		beforeNumber:    set("about"),
		notBeforeNumber: set("all", "every"),
		openers: openers(map[string]Pattern{"when": PatternEvent, "while": PatternState, "if": PatternUnwanted, "where": PatternOptional},
			"if", "once", "upon", "after", "before", "until", "unless", "whenever", "during", "on", "provided",
			"as soon as", "as long as", "in case", "in the event"),
		thenWords:      set("then"),
		triggers:       [][]string{{"when"}, {"whenever"}, {"while"}, {"as", "soon", "as"}},
		pronouns:       set("it", "its", "it's", "they", "them", "their", "theirs", "he", "she", "him", "her", "his"),
		demonstratives: set("this", "these", "those"),
		auxiliaries:    set("is", "are", "was", "were", "has", "have", "will"),
	},
	German: {
		modals: set("muss", "müssen", "muß", "soll", "sollen", "darf", "dürfen"),
		negatives: []string{"darf nicht", "dürfen nicht", "muss nicht", "müssen nicht", "soll nicht", "sollen nicht",
			"nie", "niemals"},
		beforeNumber:    set(),
		notBeforeNumber: set("alle", "jede"),
		openers: openers(map[string]Pattern{"wenn": PatternEvent, "sobald": PatternEvent, "während": PatternState,
			"solange": PatternState, "falls": PatternUnwanted, "sofern": PatternOptional, "wo": PatternOptional},
			"falls", "nachdem", "bevor", "ehe", "bis", "bei", "beim", "nach", "vor", "außer", "im falle", "im fall",
			"es sei denn"),
		thenWords: set("dann"),
		triggers:  [][]string{{"wenn"}, {"sobald"}, {"falls"}, {"während"}, {"solange"}, {"nachdem"}, {"bevor"}},
		pronouns: set("er", "sie", "es", "ihm", "ihn", "ihnen", "ihr", "ihre", "ihren", "ihrem", "ihrer", "seine",
			"seinen", "seinem", "seiner", "dessen", "deren"),
		demonstratives: set("dies", "diese", "dieser", "dieses", "diesen", "diesem"),
		auxiliaries:    set("ist", "sind", "war", "waren", "hat", "haben", "wird", "werden"),
	},
}

// English passive: a form of be and a participle, without by.
var (
	beForms = set("is", "are", "was", "were", "be", "been", "being")
	// passiveAdverbs may stand between the form of be and the participle, beside any word ending in -ly.
	passiveAdverbs = set("not", "also", "only", "always", "still", "then", "never", "first", "all")
	// irregular are participles that do not end in -ed.
	irregular = set("arisen", "awoken", "beaten", "become", "begun", "bent", "bet", "bitten", "blown", "broken",
		"brought", "broadcast", "built", "burnt", "bought", "cast", "caught", "chosen", "cut", "dealt", "done",
		"drawn", "driven", "dug", "eaten", "fallen", "fed", "felt", "fought", "found", "flown", "forbidden",
		"forecast", "foreseen", "forgiven", "forgotten", "frozen", "given", "gotten", "grown", "heard", "held",
		"hidden", "hit", "hung", "hurt", "kept", "known", "laid", "led", "learnt", "left", "lent", "let", "lit",
		"lost", "made", "meant", "met", "misled", "mislaid", "misread", "mistaken", "misunderstood", "overcome",
		"overdone", "overheard", "overridden", "overrun", "overseen", "overtaken", "overthrown", "overwritten",
		"paid", "proven", "put", "quit", "read", "rebuilt", "redone", "remade", "reread", "reset", "resent",
		"retaken", "rewritten", "ridden", "risen", "run", "rung", "said", "seen", "sent", "set", "sewn", "shaken",
		"shown", "shut", "shot", "slain", "slid", "sold", "sought", "sown", "spent", "split", "spoken", "spread",
		"spun", "stolen", "stood", "struck", "stuck", "stung", "sung", "sunk", "sworn", "swept", "swum", "taken",
		"taught", "told", "thought", "thrown", "thrust", "torn", "trodden", "understood", "undergone", "undertaken",
		"undone", "upheld", "upset", "withdrawn", "withheld", "woken", "won", "worn", "wound", "woven", "written",
		"wrung")
	// adjectival are participles that, after a form of be, describe a state rather than an action.
	adjectival = set("located", "intended", "based", "supposed", "interested", "concerned", "related", "involved",
		"connected", "dedicated", "equipped", "entitled", "satisfied", "logged", "called", "named", "labelled",
		"labeled", "titled", "bound", "tired", "aged")
)

// German passive: a form of werden and a participle in the same clause, without von or durch.
var (
	werdenForms = set("wird", "werden", "wurde", "wurden", "worden", "würde", "würden")
	agentsDE    = set("von", "vom", "durch")
	// strongDE are participles that end in -en, which a rule cannot tell from an infinitive.
	strongDE = set("geschrieben", "gelesen", "gegeben", "genommen", "gesehen", "gefunden", "gehalten", "getragen",
		"geschlossen", "gezogen", "gesprochen", "gebrochen", "gestohlen", "gefahren", "gerufen", "geladen",
		"geschnitten", "gestrichen", "gelassen", "gefangen", "geworfen", "getroffen", "getrieben", "gewonnen",
		"geboten", "gebeten", "gebunden", "gezwungen", "geschoben", "geschaffen", "gewiesen", "geliehen", "gemessen",
		"gegessen", "gestanden", "gehoben", "gesungen", "gesunken", "gemieden", "geschieden", "geglichen",
		"gegriffen", "geraten", "gerissen", "geschlagen", "geschossen", "gestoßen", "getan", "gewaschen",
		"verloren", "vergessen", "verstanden", "verborgen", "verboten", "verschoben", "verglichen", "verwiesen",
		"vertrieben", "verlassen", "vermieden", "verworfen", "verschrieben", "verbunden", "vergeben", "verliehen",
		"betrieben", "beschrieben", "behalten", "begonnen", "beschlossen", "bewiesen", "berufen", "betroffen",
		"erhalten", "erhoben", "erlassen", "erworben", "ergriffen", "erschienen", "entnommen", "entladen",
		"entschieden", "entworfen", "entlassen", "enthalten", "entzogen", "empfangen", "empfohlen", "zerbrochen",
		"unterbrochen", "unterschrieben", "unternommen", "übertragen", "übergeben", "überschrieben", "übernommen",
		"überwiesen", "übersehen")
	// notParticipleDE look like participles and are not, or are adjectives after werden.
	notParticipleDE = set("bereit", "gesamt", "gerecht", "geeignet", "gerät", "genügt", "gelingt", "gefällt",
		"gewinnt", "erhält", "enthält", "entspricht", "besteht", "entsteht", "versteht", "verhält", "betrifft",
		"beträgt", "berechtigt", "bekannt", "beliebt", "bereits", "verfügt", "erfolgt", "gewohnt")
	separableDE = []string{"wiederher", "zusammen", "herunter", "zurück", "voran", "vorbei", "weiter", "wieder",
		"bereit", "sicher", "heraus", "herein", "herauf", "heran", "herab", "hinzu", "durch", "statt", "fertig",
		"fest", "frei", "hoch", "fort", "teil", "auf", "aus", "bei", "dar", "ein", "her", "hin", "los", "mit",
		"nach", "vor", "weg", "ab", "an", "um", "zu"}
	inseparableDE = []string{"hinter", "unter", "wider", "miss", "über", "emp", "ent", "ver", "zer", "be", "er"}
)
