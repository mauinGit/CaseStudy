package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"github.com/go-playground/validator/v10/non-standard/validators"

	"github.com/mauinGit/gdgoc-core-team-api/internal/apperror"
	"github.com/mauinGit/gdgoc-core-team-api/internal/http/middleware"
)

var validatorOnce sync.Once

// SetupValidator makes validation errors report JSON/query field names and
// registers the custom "notblank" and "phone" rules.
func SetupValidator() {
	validatorOnce.Do(func() {
		v, ok := binding.Validator.Engine().(*validator.Validate)
		if !ok {
			return
		}
		v.RegisterTagNameFunc(func(f reflect.StructField) string {
			for _, tag := range []string{"json", "form", "uri"} {
				name, _, _ := strings.Cut(f.Tag.Get(tag), ",")
				if name == "-" {
					return ""
				}
				if name != "" {
					return name
				}
			}
			return f.Name
		})
		_ = v.RegisterValidation("notblank", validators.NotBlank)
		_ = v.RegisterValidation("phone", isPhone)
	})
}

// bindJSON decodes and validates a JSON body. Only fields declared in the
// request DTO are read, so extra fields (e.g. "role") are ignored.
func bindJSON(c *gin.Context, dst any) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		respondBindError(c, err)
		return false
	}
	return true
}

// bindQuery decodes and validates query parameters.
func bindQuery(c *gin.Context, dst any) bool {
	if err := c.ShouldBindQuery(dst); err != nil {
		respondBindError(c, err)
		return false
	}
	return true
}

func respondBindError(c *gin.Context, err error) {
	if middleware.IsBodyTooLarge(err) {
		apperror.Abort(c, http.StatusRequestEntityTooLarge, apperror.Detail{Code: "PAYLOAD_TOO_LARGE", Message: "Body request terlalu besar"})
		return
	}

	var verrs validator.ValidationErrors
	if errors.As(err, &verrs) {
		fields := make([]apperror.FieldError, 0, len(verrs))
		for _, fe := range verrs {
			fields = append(fields, apperror.FieldError{Field: fe.Field(), Message: fieldMessage(fe)})
		}
		apperror.Respond(c, &apperror.ValidationError{Message: "Input tidak valid", Fields: fields})
		return
	}

	var typeErr *json.UnmarshalTypeError
	switch {
	case errors.As(err, &typeErr):
		apperror.Respond(c, &apperror.ValidationError{
			Message: "Input tidak valid",
			Fields:  []apperror.FieldError{{Field: typeErr.Field, Message: "tipe data tidak sesuai"}},
		})
	case errors.Is(err, io.EOF):
		apperror.Respond(c, &apperror.ValidationError{Message: "Body request wajib diisi"})
	default:
		apperror.Respond(c, &apperror.ValidationError{Message: "Format request tidak valid"})
	}
}

func fieldMessage(fe validator.FieldError) string {
	isString := fe.Kind() == reflect.String
	switch fe.Tag() {
	case "required":
		return "wajib diisi"
	case "email":
		return "format email tidak valid"
	case "uuid", "uuid4":
		return "harus berupa UUID yang valid"
	case "min":
		if isString {
			return fmt.Sprintf("minimal %s karakter", fe.Param())
		}
		return fmt.Sprintf("minimal %s", fe.Param())
	case "max":
		if isString {
			return fmt.Sprintf("maksimal %s karakter", fe.Param())
		}
		return fmt.Sprintf("maksimal %s", fe.Param())
	case "gt":
		return fmt.Sprintf("harus lebih dari %s", fe.Param())
	case "gte":
		return fmt.Sprintf("minimal %s", fe.Param())
	case "lte":
		return fmt.Sprintf("maksimal %s", fe.Param())
	case "oneof":
		return fmt.Sprintf("harus salah satu dari: %s", strings.ReplaceAll(fe.Param(), " ", ", "))
	case "e164", "phone":
		return "format nomor telepon tidak valid"
	case "gtfield":
		return fmt.Sprintf("harus setelah %s", fe.Param())
	case "notblank":
		return "tidak boleh kosong"
	default:
		return "tidak valid"
	}
}

// phonePattern accepts local (08xx) and international (+62xx) numbers.
var phonePattern = regexp.MustCompile(`^\+?[0-9]{8,15}$`)

// isPhone validates a phone number. An empty string is allowed because it
// means "clear this field" in profile updates.
func isPhone(fl validator.FieldLevel) bool {
	s := strings.TrimSpace(fl.Field().String())
	return s == "" || phonePattern.MatchString(s)
}
