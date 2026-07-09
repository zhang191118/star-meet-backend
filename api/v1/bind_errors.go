package v1

import (
	"errors"
	"net/http"
	"reflect"

	"star-meet-backend/model/response"
	"star-meet-backend/service"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

func failBindError(c *gin.Context, err error, req interface{}) {
	if field := bindValidationField(err, req); field != "" {
		response.FailWithError(c, http.StatusUnprocessableEntity, "validation failed", "validation_failed", gin.H{
			"field":  field,
			"reason": err.Error(),
		})
		return
	}

	response.FailWithError(c, http.StatusBadRequest, "invalid JSON body", "invalid_json", gin.H{
		"field":  "body",
		"reason": err.Error(),
	})
}

func failMissingField(c *gin.Context, field string) {
	failValidationField(c, field, "required")
}

func failValidationField(c *gin.Context, field, reason string) {
	response.FailWithError(c, http.StatusUnprocessableEntity, "validation failed", "validation_failed", gin.H{
		"field":  field,
		"reason": reason,
	})
}

func failUnauthenticated(c *gin.Context) {
	response.FailWithError(c, http.StatusUnauthorized, "unauthorized", "unauthorized", nil)
}

func failInvalidUserField(c *gin.Context, err error) bool {
	var fieldErr service.InvalidUserFieldError
	if !errors.As(err, &fieldErr) {
		return false
	}
	failValidationField(c, fieldErr.Field, fieldErr.Reason)
	return true
}

func bindValidationField(err error, req interface{}) string {
	var validationErrors validator.ValidationErrors
	if !errors.As(err, &validationErrors) || len(validationErrors) == 0 {
		return ""
	}

	reqType := reflect.TypeOf(req)
	if reqType.Kind() == reflect.Ptr {
		reqType = reqType.Elem()
	}
	if reqType.Kind() != reflect.Struct {
		return validationErrors[0].Field()
	}

	if field, ok := reqType.FieldByName(validationErrors[0].StructField()); ok {
		if jsonName := jsonFieldName(field.Tag.Get("json")); jsonName != "" {
			return jsonName
		}
	}
	return validationErrors[0].Field()
}

func jsonFieldName(tag string) string {
	for i, ch := range tag {
		if ch == ',' {
			return tag[:i]
		}
	}
	return tag
}
