package module

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// APIVersion is a supported major version of the HTTP API.
type APIVersion string

const (
	V1 APIVersion = "v1"
	V2 APIVersion = "v2"
)

// Prefix returns the URL prefix for this version, e.g. "/api/v1".
func (v APIVersion) Prefix() string { return "/api/" + string(v) }

// SupportedVersions lists the versions the router mounts, in order.
//
// Introducing /api/v2 is a two-line change: add V2 here and add its case to
// mountModule. Every existing module keeps working untouched, and any module
// that wants to serve v2 opts in by implementing V2Registrar.
var SupportedVersions = []APIVersion{V1}

// Registry holds the modules of the application and mounts them onto a router.
//
// It centralises the "which module is served under which version" decision so
// that neither bootstrap nor any individual module has to reason about it.
type Registry struct {
	modules []Module
	log     *zap.Logger
}

// NewRegistry builds a registry.
func NewRegistry(log *zap.Logger) *Registry {
	return &Registry{log: log}
}

// Register adds modules in declaration order.
func (r *Registry) Register(modules ...Module) *Registry {
	r.modules = append(r.modules, modules...)
	return r
}

// Modules returns the registered modules, for the worker and for diagnostics.
func (r *Registry) Modules() []Module { return r.modules }

// Len reports how many modules are registered.
func (r *Registry) Len() int { return len(r.modules) }

// Mount attaches every module to the engine.
//
// Root routes are mounted first, then one group per supported API version. A
// module is mounted on a version only if it implements that version's
// registrar, so versions are genuinely independent rather than a naming
// convention over one shared route table.
func (r *Registry) Mount(engine *gin.Engine) {
	root := engine.Group("/")

	for _, m := range r.modules {
		if registrar, ok := m.(RootRegistrar); ok {
			registrar.RegisterRoot(root)
			r.log.Info("module mounted",
				zap.String("module", m.Name()),
				zap.String("scope", "root"),
			)
		}
	}

	for _, version := range SupportedVersions {
		group := engine.Group(version.Prefix())

		mounted := 0
		for _, m := range r.modules {
			if r.mountModule(m, version, group) {
				mounted++
			}
		}

		r.log.Info("api version mounted",
			zap.String("version", string(version)),
			zap.String("prefix", version.Prefix()),
			zap.Int("modules", mounted),
		)
	}
}

// mountModule attaches m to a single version group, reporting whether it
// implemented that version at all.
func (r *Registry) mountModule(m Module, version APIVersion, group *gin.RouterGroup) bool {
	var mounted bool

	switch version {
	case V1:
		if registrar, ok := m.(V1Registrar); ok {
			registrar.RegisterV1(group)
			mounted = true
		}
	case V2:
		if registrar, ok := m.(V2Registrar); ok {
			registrar.RegisterV2(group)
			mounted = true
		}
	}

	if mounted {
		r.log.Info("module mounted",
			zap.String("module", m.Name()),
			zap.String("scope", string(version)),
		)
	}

	return mounted
}

// Validate checks the registry for wiring mistakes that would otherwise surface
// as a silently missing endpoint in production.
//
// It is called during bootstrap and fails the process, because a module that
// registers no routes at all is almost always a forgotten interface method.
func (r *Registry) Validate() error {
	seen := make(map[string]struct{}, len(r.modules))

	for _, m := range r.modules {
		name := m.Name()

		if name == "" {
			return fmt.Errorf("module: a module returned an empty Name()")
		}
		if _, duplicate := seen[name]; duplicate {
			return fmt.Errorf("module: duplicate module name %q", name)
		}
		seen[name] = struct{}{}

		if !servesAnyRoutes(m) {
			return fmt.Errorf(
				"module %q implements no route registrar; "+
					"it must implement at least one of V1Registrar, V2Registrar or RootRegistrar",
				name,
			)
		}
	}

	return nil
}

func servesAnyRoutes(m Module) bool {
	if _, ok := m.(RootRegistrar); ok {
		return true
	}
	if _, ok := m.(V1Registrar); ok {
		return true
	}
	if _, ok := m.(V2Registrar); ok {
		return true
	}
	return false
}
