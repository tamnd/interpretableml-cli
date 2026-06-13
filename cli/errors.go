package cli

import (
	"errors"

	"github.com/tamnd/interpretableml-cli/interpretableml"
)

func isNotFound(err error) bool {
	return errors.Is(err, interpretableml.ErrNotFound)
}
