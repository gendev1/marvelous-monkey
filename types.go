package margincalc

// Side and Kind are the leg's role in the position.
type Side string
type Kind string

const (
	Long  Side = "long"
	Short Side = "short"
)

const (
	OptionKind      Kind = "option"
	StockKind       Kind = "stock"
	ETFKind         Kind = "etf"
	ETNKind         Kind = "etn"
	ConvertibleKind Kind = "convertible"
	WarrantKind     Kind = "warrant"
)

type AccountType string

const (
	CashAccount   AccountType = "cash"
	MarginAccount AccountType = "margin"
)

type Phase string

const (
	Initial     Phase = "initial"
	Maintenance Phase = "maintenance"
)

// Leg is one component of a position. Use zero values for irrelevant fields.
// Numeric fields are float64 throughout to match CEL's strict-double typing
// once values land in the rules engine.
type Leg struct {
	Side       Side    `yaml:"side" json:"side"`
	Kind       Kind    `yaml:"kind" json:"kind"`
	OptionType string  `yaml:"option_type,omitempty" json:"option_type,omitempty"` // "put" | "call"
	K          float64 `yaml:"K,omitempty"  json:"K,omitempty"`
	P          float64 `yaml:"P,omitempty"  json:"P,omitempty"`  // current option market value
	P0         float64 `yaml:"P0,omitempty" json:"P0,omitempty"` // option proceeds at trade
	Qty        float64 `yaml:"qty,omitempty"  json:"qty,omitempty"`
	Mult       float64 `yaml:"mult,omitempty" json:"mult,omitempty"` // default 100

	Style       string `yaml:"style,omitempty"        json:"style,omitempty"`        // "american" | "european"
	Venue       string `yaml:"venue,omitempty"        json:"venue,omitempty"`        // "listed" | "otc"
	SettleStyle string `yaml:"settle_style,omitempty" json:"settle_style,omitempty"` // "physical" | "cash"
	Expiration  string `yaml:"expiration,omitempty"   json:"expiration,omitempty"`   // ISO date string
	Underlying  string `yaml:"underlying,omitempty"   json:"underlying,omitempty"`

	TimeToExpirationMonths float64 `yaml:"time_to_expiration_months,omitempty" json:"time_to_expiration_months,omitempty"`
	BrokerGuaranteed       bool    `yaml:"broker_guaranteed,omitempty"         json:"broker_guaranteed,omitempty"`

	// Stock fields
	Shares            float64 `yaml:"shares,omitempty"              json:"shares,omitempty"`
	ShortSaleProceeds float64 `yaml:"short_sale_proceeds,omitempty" json:"short_sale_proceeds,omitempty"`
	SalePrice         float64 `yaml:"sale_price,omitempty"          json:"sale_price,omitempty"`

	// ETF/ETN fields
	Price        float64 `yaml:"price,omitempty"        json:"price,omitempty"`
	TracksIndex  string  `yaml:"tracks_index,omitempty" json:"tracks_index,omitempty"`
	Leveraged    bool    `yaml:"leveraged,omitempty"    json:"leveraged,omitempty"`
	KEquivalent  float64 `yaml:"K_equivalent,omitempty" json:"K_equivalent,omitempty"`
}

// Position is the input to the calculator.
type Position struct {
	Legs                    []Leg   `yaml:"legs" json:"legs"`
	U                       float64 `yaml:"U"     json:"U"`
	Class                   string  `yaml:"class" json:"class"` // rates table key
	Lev                     float64 `yaml:"lev,omitempty" json:"lev,omitempty"`
	UnderlyingIsEquityBased bool    `yaml:"underlying_is_equity_based,omitempty" json:"underlying_is_equity_based,omitempty"`
}

// Result is the output of one evaluation.
// Result is the output of one evaluation.
//
// The Cboe manual distinguishes three numbers per position:
//
//	Requirement      gross "Margin Requirement" before short proceeds are applied
//	AppliedProceeds  short-option premium credit received when putting the trade on
//	CashCall         = Requirement - AppliedProceeds; the SMA debit / net cash
//	                   the customer must actually deposit
//
// Rules in the YAML express `initial`/`maintenance` as the gross requirement,
// and `initial_proceeds`/`maintenance_proceeds` as the credit. CashCall is
// computed by the engine. AppliedProceeds defaults to 0 when a rule has no
// proceeds expression (e.g. long-only positions).
type Result struct {
	RuleID          string  `json:"rule_id"`
	FormulaKey      string  `json:"formula_key,omitempty"` // e.g. "margin.initial"
	AccountType     string  `json:"account_type"`
	Phase           string  `json:"phase"`
	Requirement     float64 `json:"requirement,omitempty"`
	AppliedProceeds float64 `json:"applied_proceeds,omitempty"`
	CashCall        float64 `json:"cash_call,omitempty"`
	Permitted       bool    `json:"permitted"`
	DepositKind     string  `json:"deposit_kind,omitempty"` // "cash_or_escrow" | "underlying_or_escrow" | etc.
}

// ToMap converts a Leg to a CEL-friendly map. Empty strings/zero-values are
// kept so CEL access never errors on missing keys.
func (l Leg) toMap() map[string]any {
	return map[string]any{
		"side":                       string(l.Side),
		"kind":                       string(l.Kind),
		"option_type":                l.OptionType,
		"K":                          l.K,
		"P":                          l.P,
		"P0":                         l.P0,
		"qty":                        l.Qty,
		"mult":                       l.Mult,
		"style":                      l.Style,
		"venue":                      l.Venue,
		"settle_style":               l.SettleStyle,
		"expiration":                 l.Expiration,
		"underlying":                 l.Underlying,
		"time_to_expiration_months":  l.TimeToExpirationMonths,
		"broker_guaranteed":          l.BrokerGuaranteed,
		"shares":                     l.Shares,
		"short_sale_proceeds":        l.ShortSaleProceeds,
		"sale_price":                 l.SalePrice,
		"price":                      l.Price,
		"tracks_index":               l.TracksIndex,
		"leveraged":                  l.Leveraged,
		"K_equivalent":               l.KEquivalent,
	}
}
