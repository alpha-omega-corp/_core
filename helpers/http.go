package helpers

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/uptrace/bunrouter"
)

type GetOne struct {
	Id int64 `json:"id"`
}

/*
Route adapters.

A controller method is nearly always the same four moves: pull the id off the
route, decode the body, hand both to a handler, encode what comes back. Written
out per route that is five lines of ceremony around one call, and the ceremony
is where the mistakes live — the wrong param type, a validation branch that
forgets to return, an error mapped to the wrong status.

These take the handler method itself, so a controller says which handler runs
and nothing else:

	func (c *SocialLinkController) GetOne() bunrouter.HandlerFunc {
		return helpers.HandleOne(c.Handler().GetOne)
	}

The handler method is read when the route is registered, not when it is
called, so the controller must already have been through RegisterControllers —
which is the same requirement the previous form had, and the same symptom if
it is missed.

Validation is variadic because most writes have none. When a validator
returns an error the request stops at 400 and the handler is never reached;
anything the handler itself returns is a 500, because by then the input was
acceptable and it is the server that failed.
*/

// Handle adapts a read that needs nothing from the request.

func check[B any](data *B, validators []func(data *B) error) error {
	for _, validate := range validators {
		if err := validate(data); err != nil {
			return err
		}
	}

	return nil
}

func GetBody[T any](w http.ResponseWriter, req bunrouter.Request) *T {
	data := new(T)

	if err := json.NewDecoder(req.Body).Decode(data); err != nil {
		Error(w, err, http.StatusBadRequest)
	}

	return data
}

func GetParams[T any](w http.ResponseWriter, req bunrouter.Request) *T {
	paramsMap := req.Params().Map()
	paramsData := make(map[string]interface{}, len(paramsMap))

	for key, value := range paramsMap {
		paramsData[key] = value
	}

	if idStr, ok := paramsData["id"].(string); ok {
		if idInt, err := strconv.ParseInt(idStr, 10, 64); err == nil {
			paramsData["id"] = idInt
		}
	}

	params, err := json.Marshal(paramsData)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
	}

	data := new(T)
	if err := json.Unmarshal(params, data); err != nil {
		w.WriteHeader(http.StatusBadRequest)
	}

	return data
}

func Response[T any](w http.ResponseWriter, req func() (*T, error)) error {
	res, err := req()
	return JSON(w, res, err)
}

func JSON[T any](w http.ResponseWriter, res *T, err error) error {
	if err != nil {
		Error(w, err, http.StatusInternalServerError)
	}

	return bunrouter.JSON(w, res)
}

func Error(w http.ResponseWriter, err error, code int) {
	w.WriteHeader(code)

	log.Printf("%+v\n", err)
	_, _ = w.Write([]byte(err.Error()))
}
