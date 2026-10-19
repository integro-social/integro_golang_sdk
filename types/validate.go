package validate

import (
	"regexp"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

type Preprocess int

const (
	PreprocessTrim Preprocess = iota
	PreprocessLowercase
	PreprocessUppercase
)

type Constraint struct {
	Kind   string
	Int    int
	Bound  int64
	Source string
	Hint   string
}

type ValidationSpec struct {
	Preprocess  []Preprocess
	Constraints []Constraint
}

type Violation struct {
	Kind      string
	Min       int
	Max       int
	Expected  int
	Actual    int
	Bound     int64
	Hint      string
	Position  int
	Character string
}

func (v Violation) Message() string {
	switch v.Kind {
	case "tooShort":
		return "muito curto (mínimo " + itoa(v.Min) + " caracteres)"
	case "tooLong":
		return "muito longo (máximo " + itoa(v.Max) + " caracteres)"
	case "exactLength":
		return "tamanho inválido (esperado " + itoa(v.Expected) + ", recebido " + itoa(v.Actual) + ")"
	case "tooFewBytes":
		return "muito curto (mínimo " + itoa(v.Min) + " bytes)"
	case "tooManyBytes":
		return "muito longo (máximo " + itoa(v.Max) + " bytes)"
	case "tooFewDigits":
		return "muito curto (mínimo " + itoa(v.Min) + " dígitos)"
	case "tooManyDigits":
		return "muito longo (máximo " + itoa(v.Max) + " dígitos)"
	case "tooFewItems":
		return "poucos itens (mínimo " + itoa(v.Min) + ")"
	case "tooManyItems":
		return "muitos itens (máximo " + itoa(v.Max) + ")"
	case "invalidFormat":
		if v.Hint != "" {
			return "formato inválido (" + v.Hint + ")"
		}
		return "formato inválido"
	case "invalidChars":
		return "caracteres inválidos"
	case "invalidCharAt":
		return "caractere inválido na posição " + itoa(v.Position) + " ('" + v.Character + "')"
	case "notDigits":
		return "deve conter apenas dígitos"
	case "notANumber":
		return "deve ser um número"
	case "notABoolean":
		return "deve ser verdadeiro ou falso"
	case "tooSmall":
		return "muito pequeno (mínimo " + i64toa(v.Bound) + ")"
	case "tooLarge":
		return "muito grande (máximo " + i64toa(v.Bound) + ")"
	case "missingUppercase":
		return "deve conter uma letra maiúscula"
	case "missingLowercase":
		return "deve conter uma letra minúscula"
	case "missingDigit":
		return "deve conter um dígito"
	case "missingSymbol":
		return "deve conter um símbolo"
	}
	return ""
}

func digitCount(s string) int {
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n++
		}
	}
	return n
}

func hasRune(s string, f func(rune) bool) bool {
	for _, c := range s {
		if f(c) {
			return true
		}
	}
	return false
}

func normalize(spec ValidationSpec, s string) string {
	for _, step := range spec.Preprocess {
		switch step {
		case PreprocessTrim:
			s = strings.TrimSpace(s)
		case PreprocessLowercase:
			s = asciiLower(s)
		case PreprocessUppercase:
			s = asciiUpper(s)
		}
	}
	return s
}

func asciiLower(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, s)
}

func asciiUpper(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' {
			return r - ('a' - 'A')
		}
		return r
	}, s)
}

var regexCache sync.Map

func regexFor(source string) *regexp.Regexp {
	if cached, ok := regexCache.Load(source); ok {
		return cached.(*regexp.Regexp)
	}
	re := regexp.MustCompile(source)
	regexCache.Store(source, re)
	return re
}

func asInt64(value interface{}) (int64, bool) {
	const maxInt64 = int64(^uint64(0) >> 1)
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int8:
		return int64(v), true
	case int16:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	case uint:
		if uint64(v) > uint64(maxInt64) {
			return maxInt64, true
		}
		return int64(v), true
	case uint8:
		return int64(v), true
	case uint16:
		return int64(v), true
	case uint32:
		return int64(v), true
	case uint64:
		if v > uint64(maxInt64) {
			return maxInt64, true
		}
		return int64(v), true
	}
	return 0, false
}

func checkOne(c Constraint, value interface{}) *Violation {
	s, _ := value.(string)
	switch c.Kind {
	case "minLen":
		if utf8.RuneCountInString(s) < c.Int {
			return &Violation{Kind: "tooShort", Min: c.Int}
		}
	case "maxLen":
		if utf8.RuneCountInString(s) > c.Int {
			return &Violation{Kind: "tooLong", Max: c.Int}
		}
	case "exactLen":
		if actual := utf8.RuneCountInString(s); actual != c.Int {
			return &Violation{Kind: "exactLength", Expected: c.Int, Actual: actual}
		}
	case "minBytes":
		if len(s) < c.Int {
			return &Violation{Kind: "tooFewBytes", Min: c.Int}
		}
	case "maxBytes":
		if len(s) > c.Int {
			return &Violation{Kind: "tooManyBytes", Max: c.Int}
		}
	case "minDigits":
		if digitCount(s) < c.Int {
			return &Violation{Kind: "tooFewDigits", Min: c.Int}
		}
	case "maxDigits":
		if digitCount(s) > c.Int {
			return &Violation{Kind: "tooManyDigits", Max: c.Int}
		}
	case "noControlChars":
		if hasRune(s, unicode.IsControl) {
			return &Violation{Kind: "invalidChars"}
		}
	case "asciiDigitsOnly":
		for _, r := range s {
			if r < '0' || r > '9' {
				return &Violation{Kind: "notDigits"}
			}
		}
	case "regex":
		if !regexFor(c.Source).MatchString(s) {
			return &Violation{Kind: "invalidFormat", Hint: c.Hint}
		}
	case "requireUppercase":
		if !hasRune(s, unicode.IsUpper) {
			return &Violation{Kind: "missingUppercase"}
		}
	case "requireLowercase":
		if !hasRune(s, unicode.IsLower) {
			return &Violation{Kind: "missingLowercase"}
		}
	case "requireDigit":
		if !hasRune(s, func(r rune) bool { return r >= '0' && r <= '9' }) {
			return &Violation{Kind: "missingDigit"}
		}
	case "requireSymbol":
		if !hasRune(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }) {
			return &Violation{Kind: "missingSymbol"}
		}
	case "min":
		if n, ok := asInt64(value); ok && n < c.Bound {
			return &Violation{Kind: "tooSmall", Bound: c.Bound}
		}
	case "max":
		if n, ok := asInt64(value); ok && n > c.Bound {
			return &Violation{Kind: "tooLarge", Bound: c.Bound}
		}
	}
	return nil
}

func Check(spec ValidationSpec, value interface{}) *Violation {
	if s, ok := value.(string); ok {
		value = normalize(spec, s)
	}
	for _, c := range spec.Constraints {
		if v := checkOne(c, value); v != nil {
			return v
		}
	}
	return nil
}

func CheckAll(spec ValidationSpec, value interface{}) []Violation {
	if s, ok := value.(string); ok {
		value = normalize(spec, s)
	}
	out := []Violation{}
	for _, c := range spec.Constraints {
		if v := checkOne(c, value); v != nil {
			out = append(out, *v)
		}
	}
	return out
}

func itoa(n int) string   { return i64toa(int64(n)) }
func i64toa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
