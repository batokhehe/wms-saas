package postgres

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// TranslateError converts a GORM/driver error into the application's error
// vocabulary.
//
// Every repository method funnels its error return through this function. That
// is what keeps persistence details out of the service layer: a service asks
// "is this a conflict?" via errors.Is and never inspects a Postgres SQLSTATE.
//
// It also closes a real leak: without translation, a unique-constraint failure
// would surface as a 500 carrying the constraint name — exposing the schema to
// whoever triggered it.
func TranslateError(err error, op string) error {
	if err == nil {
		return nil
	}

	// Already classified upstream; keep the original classification.
	var appErr *apperror.Error
	if errors.As(err, &appErr) {
		return appErr
	}

	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return apperror.NotFound("The requested resource was not found").
			WithCause(err).WithOp(op)

	case errors.Is(err, gorm.ErrDuplicatedKey):
		return apperror.Conflict("A resource with these values already exists").
			WithCause(err).WithOp(op)

	case errors.Is(err, gorm.ErrForeignKeyViolated):
		return apperror.Conflict("The operation references a resource that does not exist").
			WithCause(err).WithOp(op)

	case errors.Is(err, gorm.ErrInvalidTransaction), errors.Is(err, gorm.ErrInvalidDB):
		return apperror.Internal("A database error occurred").
			WithCause(err).WithOp(op)

	// A cancelled request should not be logged as a server fault: the caller
	// hung up, and nothing is broken.
	case errors.Is(err, context.Canceled):
		return apperror.New(apperror.CodeTimeout, 499, "The request was cancelled").
			WithCause(err).WithOp(op)

	case errors.Is(err, context.DeadlineExceeded):
		return apperror.Timeout("The database operation timed out").
			WithCause(err).WithOp(op)

	default:
		return apperror.Internal("A database error occurred").
			WithCause(err).WithOp(op)
	}
}
