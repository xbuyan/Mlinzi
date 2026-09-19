package rag

// stopwords is a small, closed-class list of function words across the
// three interface languages. It exists because BM25's IDF cannot learn that
// "what" or "next" are meaningless on a corpus this small: with 769 chunks,
// a word appearing in ten of them scores as highly selective as a genuinely
// distinctive term, which is exactly how "lottery numbers for next week"
// once retrieved the "Next steps" section of a policing guide. A fixed
// closed-class list — pronouns, question words, conjunctions, particles —
// is auditable by inspection, unlike a statistical stopword list, and its
// whole size is small enough to read at a glance.
//
// Kiswahili function words are deliberately included even though the
// English ones are the ones that bit: a question in any supported language
// goes through the same gate, and "nini" ("what") would bite a Kiswahili
// question exactly the same way.
var stopwords = map[string]bool{ // English closed class.
	"a": true, "about": true, "after": true, "against": true, "all": true,
	"an": true, "and": true, "any": true, "are": true, "as": true, "at": true,
	"be": true, "been": true, "before": true, "being": true, "between": true,
	"both": true, "but": true, "by": true, "can": true, "did": true, "do": true,
	"does": true, "during": true, "each": true, "for": true, "from": true,
	"had": true, "has": true, "have": true, "how": true, "i": true, "if": true,
	"in": true, "into": true, "is": true, "it": true, "its": true, "me": true,
	"my": true, "next": true, "no": true, "not": true, "of": true, "off": true,
	"on": true, "only": true, "or": true, "other": true, "our": true, "out": true,
	"over": true, "same": true, "should": true, "so": true, "some": true,
	"such": true, "than": true, "that": true, "the": true, "their": true,
	"them": true, "then": true, "there": true, "these": true, "they": true,
	"this": true, "through": true, "to": true, "too": true, "under": true,
	"until": true, "up": true, "very": true, "was": true, "we": true,
	"were": true, "what": true, "when": true, "where": true, "which": true,
	"while": true, "who": true, "whom": true, "why": true, "will": true,
	"with": true, "without": true, "within": true, "would": true, "yet": true,
	"you": true, "your": true,
	// French closed class.
	"au": true, "aux": true, "avec": true, "ce": true, "ces": true, "comment": true,
	"dans": true, "de": true, "des": true, "du": true, "elle": true, "elles": true,
	"en": true, "est": true, "et": true, "eux": true, "il": true, "ils": true,
	"la": true, "le": true, "les": true, "leur": true, "lui": true,
	"ma": true, "mais": true, "mes": true, "mon": true, "ne": true,
	"nos": true, "notre": true, "nous": true, "ou": true, "par": true,
	"pas": true, "pour": true, "qu": true, "que": true, "qui": true, "quoi": true,
	"sa": true, "sans": true, "se": true, "ses": true, "son": true, "sur": true,
	"ta": true, "te": true, "tes": true, "ton": true, "un": true, "une": true,
	"vos": true, "votre": true, "vous": true,
	// Kiswahili closed class.
	"cha": true, "hii": true, "hivyo": true, "huo": true, "iweze": true,
	"kama": true, "katika": true, "kwa": true, "kwamba": true,
	"lako": true, "langu": true, "mimi": true, "nini": true, "ni": true,
	"sasa": true, "sisi": true, "upya": true, "wake": true, "wako": true,
	"wao": true, "yake": true, "yangu": true, "yao": true, "ye": true,
	"yeye": true,
}

// isStopword reports whether t is a closed-class function word. Kept as a
// function so the mechanism has a name; if stopword handling ever grows
// beyond a fixed list, it grows here and nowhere else.
func isStopword(t string) bool {
	return stopwords[t]
}
