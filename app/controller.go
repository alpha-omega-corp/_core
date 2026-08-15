package app

import (
	"reflect"

	"github.com/uptrace/bun"
)

// Deps carries the dependencies a Handler needs to wire itself up in New.
type Deps struct {
	DB     *bun.DB
	Config *Config
}

// Handler is implemented by the concrete type a BaseController manages.
// New receives Deps and wires the handler's own fields from it (db,
// config, repositories, ...).
type Handler interface {
	New(deps Deps)
}

// BaseController owns a handler and builds it itself: Setup allocates it
// and lets it wire its dependencies via New.
//
// PT must be a pointer type implementing Handler (e.g. *MeatHandler). Go
// generics has no way to say "T, and separately the pointer to T" from a
// single parameter at compile time, so Setup falls back to reflection: a
// zero PT is a typed nil pointer, so reflect.TypeOf still reports its
// concrete type; Elem() gives the pointee type, and reflect.New allocates
// one. This trades the compile-time guarantee that PT is a pointer for a
// runtime panic if it isn't.
type BaseController[PT Handler] struct {
	handler PT
}

// Setup allocates the controller's handler and wires its dependencies.
func (c *BaseController[PT]) Setup(deps Deps) {
	var zero PT
	elem := reflect.TypeOf(zero).Elem()
	c.handler = reflect.New(elem).Interface().(PT)
	c.handler.New(deps)
}

// Handler returns the handler Setup built, for the embedding controller to
// call methods on.
func (c *BaseController[PT]) Handler() PT {
	return c.handler
}

// Controller is the minimal shape Register needs to bring a controller up.
type Controller interface {
	Setup(deps Deps)
}

func RegisterControllers(deps Deps, controllers ...Controller) {
	for _, c := range controllers {
		c.Setup(deps)
	}
}
