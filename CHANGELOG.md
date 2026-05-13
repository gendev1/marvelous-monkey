# Changelog

## Unreleased

### Changed — numerical output

- **`short_index_call_long_etf` (Cboe manual p.14): `K_equivalent` is now read from the long ETF leg (`le`) instead of the short index call leg (`sc`).** The maintenance formula is now `0.25 * min(legs.le.price, legs.le.K_equivalent) * legs.le.shares`, and the rule-level positivity validator targets `le.K_equivalent` rather than `sc.K_equivalent`. Downstream reconciliation consumers that populated `K_equivalent` on the short option leg must move it onto the ETF leg; positions with `le.K_equivalent == 0` now fail validation explicitly rather than silently producing a zero maintenance requirement. No other rule's numeric output changes.
