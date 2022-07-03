package wire

import "github.com/google/wire/internal/wire"

// Generate performs dependency injection for the packages that match the given
// patterns, return a GenerateResult for each package.
// See github.com/google/wire/internal/wire.Generate
var Generate = wire.Generate
