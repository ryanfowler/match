package match

import "github.com/ryanfowler/match/internal/patternscan"

const (
	tokenLiteral  = patternscan.TokenLiteral
	tokenParam    = patternscan.TokenParam
	tokenCatchAll = patternscan.TokenCatchAll
)

type token = patternscan.Token
