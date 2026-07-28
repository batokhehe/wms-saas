// Package validator provides request binding and validation shared by every
// module, so that a malformed request produces an identical error envelope no
// matter which endpoint received it.
package validator

import (
	"reflect"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

var once sync.Once

// Init configures the validator instance Gin uses for binding.
//
// It must be called once during bootstrap, before the server starts. The
// registration it performs is global to Gin, so doing it lazily per-request
// would be a data race.
func Init() {
	once.Do(func() {
		v, ok := binding.Validator.Engine().(*validator.Validate)
		if !ok {
			return
		}

		// Report the JSON field name rather than the Go struct field name.
		// Without this, a client that sent {"sku_code": ...} is told that
		// "SKUCode" is invalid — a name that appears nowhere in its request.
		v.RegisterTagNameFunc(func(field reflect.StructField) string {
			name := strings.SplitN(field.Tag.Get("json"), ",", 2)[0]
			if name == "-" {
				return ""
			}
			if name == "" {
				return field.Name
			}
			return name
		})
	})
}

// BindJSON binds and validates a JSON request body into target.
//
// It returns an *apperror.Error that is already correctly classified: a
// malformed body is a 400, a well-formed body that breaks a rule is a 422 with
// per-field details. Handlers therefore never interpret binding errors
// themselves — they bind, and on error hand the result to response.Error.
func BindJSON(c *gin.Context, target any) error {
	if err := c.ShouldBindJSON(target); err != nil {
		return apperror.FromValidator(err).WithOp("http.bind.json")
	}
	return nil
}

// BindQuery binds and validates query-string parameters into target.
func BindQuery(c *gin.Context, target any) error {
	if err := c.ShouldBindQuery(target); err != nil {
		return apperror.FromValidator(err).WithOp("http.bind.query")
	}
	return nil
}

// BindURI binds and validates path parameters into target.
func BindURI(c *gin.Context, target any) error {
	if err := c.ShouldBindUri(target); err != nil {
		return apperror.FromValidator(err).WithOp("http.bind.uri")
	}
	return nil
}

// Struct validates an already-populated struct, for rules that cannot be
// expressed as binding tags.
func Struct(target any) error {
	v, ok := binding.Validator.Engine().(*validator.Validate)
	if !ok {
		return nil
	}

	if err := v.Struct(target); err != nil {
		return apperror.FromValidator(err)
	}
	return nil
}
