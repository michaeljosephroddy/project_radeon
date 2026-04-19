package support

import "errors"

var errExpiryNotFuture = errors.New("expires_at must be in the future")
