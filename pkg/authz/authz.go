package authz

type Authorizer interface{
	Authorize(token interface{}, path string, msg ) error
}
