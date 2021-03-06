package mock

import (
	"os"

	"github.com/getfider/fider/app"
	"github.com/getfider/fider/app/models"
	"github.com/getfider/fider/app/pkg/email"
	"github.com/getfider/fider/app/pkg/oauth"
	"github.com/getfider/fider/app/storage/inmemory"
)

// DemoTenant is a mocked tenant
var DemoTenant *models.Tenant

// AvengersTenant is a mocked tenant
var AvengersTenant *models.Tenant

// JonSnow is a mocked user
var JonSnow *models.User

// AryaStark is a mocked user
var AryaStark *models.User

// NewSingleTenantServer creates a new multitenant test server
func NewSingleTenantServer() (*Server, *app.Services) {
	services := createServices(false)
	server := createServer(services)
	os.Setenv("HOST_MODE", "single")
	return server, services
}

// NewServer creates a new server and services for HTTP testing
func NewServer() (*Server, *app.Services) {
	services := createServices(true)
	server := createServer(services)
	os.Setenv("HOST_MODE", "multi")
	return server, services
}

func createServices(seed bool) *app.Services {
	services := &app.Services{
		Tenants: &inmemory.TenantStorage{},
		Users:   &inmemory.UserStorage{},
		Tags:    inmemory.NewTagStorage(),
		Ideas:   inmemory.NewIdeaStorage(),
		OAuth:   &OAuthService{},
		Emailer: email.NewNoopSender(),
	}

	if seed {
		DemoTenant, _ = services.Tenants.Add("Demonstration", "demo", models.TenantActive)
		AvengersTenant, _ = services.Tenants.Add("Avengers", "avengers", models.TenantActive)

		JonSnow = &models.User{
			Name:   "Jon Snow",
			Email:  "jon.snow@got.com",
			Tenant: DemoTenant,
			Role:   models.RoleAdministrator,
			Providers: []*models.UserProvider{
				{UID: "FB1234", Name: oauth.FacebookProvider},
			},
		}
		services.Users.Register(JonSnow)

		AryaStark = &models.User{Name: "Arya Stark", Email: "arya.stark@got.com", Tenant: DemoTenant, Role: models.RoleVisitor}
		services.Users.Register(AryaStark)
	}

	return services
}
