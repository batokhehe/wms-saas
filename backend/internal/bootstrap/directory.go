package bootstrap

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	authrepo "github.com/batokhehe/wms-saas/backend/internal/module/auth/repository"
	"github.com/batokhehe/wms-saas/backend/internal/module/tenancy/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
)

// userDirectory adapts the identity module's user repository to the tenancy
// module's service.UserDirectory interface.
//
// # Why the adapter lives in bootstrap
//
// Bootstrap is the composition root — the one place that is permitted to know
// about every module, and whose entire job is joining them. Putting the adapter
// here means:
//
//   - tenancy never imports auth. It declares the narrow interface it needs
//     (one method, returning one id) and receives an implementation.
//   - auth is not modified. It exposes no new accessor and gains no knowledge
//     that tenancy exists.
//
// The alternative — adding a Users() accessor to auth.Module — would have been
// a change to Authentication, and would have widened auth's public surface to
// serve a consumer it should not know about.
//
// It constructs its own instance of auth's user repository rather than sharing
// one. The repository is stateless (a *gorm.DB and an id generator), so a
// second instance costs nothing and avoids reaching into auth's private graph.
type userDirectory struct {
	users authrepo.UserRepository
}

var _ service.UserDirectory = (*userDirectory)(nil)

func newUserDirectory(db *gorm.DB, ids port.IDGenerator) *userDirectory {
	return &userDirectory{users: authrepo.NewUserRepository(db, ids)}
}

// FindIDByEmail returns the account id for an address.
//
// Only the id crosses the boundary. The tenancy module has no business seeing a
// password hash, a status or a name, and an interface that returned the whole
// user would hand it all three.
func (d *userDirectory) FindIDByEmail(ctx context.Context, email string) (uuid.UUID, error) {
	user, err := d.users.FindByEmail(ctx, email)
	if err != nil {
		// The NOT_FOUND from the repository passes through unchanged; the
		// invite flow converts it into a deliberately vague message so this
		// endpoint cannot be used to probe which addresses are registered.
		return uuid.Nil, err
	}

	return user.ID, nil
}
