package ApiServices

import(

	"github.com/emicklei/go-restful"

	"github.com/jackc/pgx"
	"./../../../utilities/userDBHandler"

	"./../../../utilities/mailer"

	"./../../../utilities/recaptcha"

	"net/http"
	"log"
)

const BadUserName string = "User lookup failed"
const BadPassword string = "Invalid password, needs to be >10 characters alphanumeric"
const BadSessionKey string = "Invalid Session Key"
const BadCredentials string = "Invalid Credentials"
const BadCaptcha string = "Invalid Re-Captcha"
const BadTradeContents string = "Invalid trade contents"

const SignupFailure string = "Failed to create user"
const BodyReadFailure string = "Failed to parse body parameter"

const DBfailure string = "Database read failed"

const mailGunMetaLoc string = "mailgunMeta.json"
const recaptchaMetaLoc string = "recaptchaMeta.json"

type UserService struct{

	pool *pgx.ConnPool
	Service *restful.WebService
	logger *log.Logger

	mailer *mailer.Mailer
	validator *recaptcha.Validator

}

// Returns a fresh UserService ready to be hooked up to restful
func NewUserService() *UserService {
	
	// Get necessary loggers
	userLogger:= GetLogger("userLogger.txt", "userLog")

	// Grab a connection pool to the DB
	pool, err:= userDB.Connect()
	if err != nil {
		userLogger.Fatalln("Failed to acquire connection to remote db", err)
	}

	aService:= UserService{
		logger: userLogger,
		pool: pool,
	}

	// Acquire and set up all requisites for sending mail
	aService.setupMailing(mailGunMetaLoc)

	// Make sure we can rate limit expensive operations
	aService.setupRecaptcha(recaptchaMetaLoc)

	// Finally, register the service
	err = aService.register()
	if err!=nil {
		userLogger.Fatalln("Failed to register UserService, ", err)
	}

	return &aService

}

// Builds all the mailing systems needed for a Users node to function.
//
// Mailer templates can be used from the mailer by referencing the template
// and providing a struct suitable for filling it.
func (aService *UserService) setupMailing(metaLoc string) {

	mailer, err:= mailer.GetMailerFromFile(mailGunMetaLoc)
	if err!=nil {
		aService.logger.Fatalln("Failed to get mailer", err)
	}

	aService.mailer = mailer

}

// Readies our ability to accept recaptcha 2.0 responses.
func (aService *UserService) setupRecaptcha(loc string) {
	validator, err:=  recaptcha.GetValidatorFromFile(loc)
	if err!=nil {
		aService.logger.Fatalln("Failed to get recaptcha validator", err)
	}

	aService.validator = validator
}

func (aService *UserService) register() error {
	
	// Ensures we have a valid filter for card names/sets
	//
	// Other services may do this but better to take an extra .1s at
	// startup than to risk nuking every attempt at adding a trade.
	err:= populateCardMaps()
	if err!=nil {
		aService.logger.Fatalln("Failed to acquire ")
	}

	userService:= new(restful.WebService)
	userService.
		Path("/api/Users").
		Consumes(restful.MIME_JSON).
		Produces(restful.MIME_JSON)

	// Extremely gross code, which does documents itself
	// in an externally packaged pretty ui, follows.

	userService.Route(userService.
		POST("/{userName}").To(aService.createUser).
		// Docs
		Doc("Attempts to create a user").
		Operation("createUser").
		Param(userService.PathParameter("userName",
			"The name that identifies a user to our service").DataType("string")).
		Reads(NewUserData{}).
		Writes("string").
		Returns(http.StatusBadRequest, SignupFailure, nil).
		Returns(http.StatusBadRequest, BadPassword, nil).
		Returns(http.StatusBadRequest, BadCaptcha, nil).
		Returns(http.StatusOK, "A valid session code for the user", nil))

	userService.Route(userService.
		POST("/{userName}/Login").To(aService.loginUser).
		// Docs
		Doc("Attempts to login a user").
		Operation("loginUser").
		Param(userService.PathParameter("userName",
			"The name that identifies a user to our service").DataType("string")).
		Reads(PasswordBody{}).
		Returns(http.StatusBadRequest, SignupFailure, nil).
		Returns(http.StatusBadRequest, BadCaptcha, nil).
		Returns(http.StatusOK, "A valid session code for the user", nil))

	userService.Route(userService.
		GET("/{userName}/Collections/GetPublic").To(aService.getUserPublicCollections).
		// Docs
		Doc("Returns a list of collections designated public by that user").
		Operation("getUserPublicCollections").
		Param(userService.PathParameter("userName",
			"The name that identifies a user to our service").DataType("string")).
		Writes([]string{}).
		Returns(http.StatusBadRequest, BadUserName, nil).
		Returns(http.StatusOK, "Public collections for a specified user", nil))

	userService.Route(userService.
		POST("/{userName}/Collections/Get").To(aService.getUserCollections).
		// Docs
		Doc("Returns a list of collections an authenticated user").
		Operation("getUserCollections").
		Param(userService.PathParameter("userName",
			"The name that identifies a user to our service").DataType("string")).
		Reads(SessionKeyBody{}).
		Writes([]string{}).
		Returns(http.StatusBadRequest, BodyReadFailure, nil).
		Returns(http.StatusUnauthorized, BadCredentials, nil).
		Returns(http.StatusOK, "Collections for a specified user", nil))

	userService.Route(userService.
		POST("/{userName}/Collections/{collectionName}/Create").
		To(aService.newCollection).
		// Docs
		Doc("Adds a new collection with the given name to the user").
		Operation("getUserCollections").
		Param(userService.PathParameter("userName",
			"The name that identifies a user to our service").DataType("string")).
		Param(userService.PathParameter("collectionName",
			"The name of a collection for that user").DataType("string")).
		Reads(SessionKeyBody{}).
		Writes(true).
		Returns(http.StatusBadRequest, BodyReadFailure, nil).
		Returns(http.StatusUnauthorized, BadCredentials, nil).
		Returns(http.StatusOK, "Collection is added", nil))

	userService.Route(userService.
		POST("/{userName}/Collections/{collectionName}/Get").
		To(aService.getCollection).
		// Docs
		Doc("Attempts to retrieve a collection from an authenticated user").
		Operation("getUserCollections").
		Param(userService.PathParameter("userName",
			"The name that identifies a user to our service").DataType("string")).
		Param(userService.PathParameter("collectionName",
			"The name of a collection for that user").DataType("string")).
		Reads(SessionKeyBody{}).
		Writes(CollectionContents{}).
		Returns(http.StatusBadRequest, BodyReadFailure, nil).
		Returns(http.StatusUnauthorized, BadCredentials, nil).
		Returns(http.StatusOK, "Collection is returned", nil))

	userService.Route(userService.
		GET("/{userName}/Collections/{collectionName}/GetPublic").
		To(aService.getCollectionPublic).
		// Docs
		Doc("Attempts to read a public collection for a user").
		Operation("getUserCollections").
		Param(userService.PathParameter("userName",
			"The name that identifies a user to our service").DataType("string")).
		Param(userService.PathParameter("collectionName",
			"The name of a collection for that user").DataType("string")).
		Writes(CollectionContents{}).
		Returns(http.StatusBadRequest, BodyReadFailure, nil).
		Returns(http.StatusUnauthorized, BadCredentials, nil).
		Returns(http.StatusOK, "Collection is returned", nil))

	userService.Route(userService.
		PATCH("/{userName}/Collections/{collectionName}/Permissions").
		To(aService.setCollectionPermissions).
		// Docs
		Doc("Attempt to change public viewing permissions for a collection").
		Operation("setCollectionPermissions").
		Param(userService.PathParameter("userName",
			"The name that identifies a user to our service").DataType("string")).
		Param(userService.PathParameter("collectionName",
			"The name of a collection for that user").DataType("string")).
		Reads(PermissionChangeBody{}).
		Returns(http.StatusBadRequest, BodyReadFailure, nil).
		Writes(true).
		Returns(http.StatusUnauthorized, BadCredentials, nil).
		Returns(http.StatusOK, "Permissions changed", nil))

	userService.Route(userService.
		POST("/{userName}/Collections/{collectionName}/Permissions").
		To(aService.getCollectionPermissions).
		// Docs
		Doc("Attempt to acquire public viewing permissions for a collection").
		Operation("getCollectionPermissions").
		Param(userService.PathParameter("userName",
			"The name that identifies a user to our service").DataType("string")).
		Param(userService.PathParameter("collectionName",
			"The name of a collection for that user").DataType("string")).
		Reads(SessionKeyBody{}).
		Returns(http.StatusBadRequest, BodyReadFailure, nil).
		Writes("").
		Returns(http.StatusUnauthorized, BadCredentials, nil).
		Returns(http.StatusOK, "Permissions changed", nil))

	userService.Route(userService.
		POST("/{userName}/Collections/{collectionName}/Trades").
		To(aService.addTrade).
		// Docs
		Doc("Attempt to add a provided trade to a collection").
		Operation("addTrade").
		Param(userService.PathParameter("userName",
			"The name that identifies a user to our service").DataType("string")).
		Param(userService.PathParameter("collectionName",
			"The name of a collection for that user").DataType("string")).
		Reads(TradeAddBody{}).
		Returns(http.StatusBadRequest, BodyReadFailure, nil).
		Writes(true).
		Returns(http.StatusUnauthorized, BadCredentials, nil).
		Returns(http.StatusOK, "Trade Added", nil))

	userService.Route(userService.
		POST("/{userName}/PasswordResetRequest").
		To(aService.requestPasswordReset).
		// Docs
		Doc("Attempts to get a password reset email sent to the user's email").
		Operation("passwordResetRequest").
		Param(userService.PathParameter("userName",
			"The name that identifies a user to our service").DataType("string")).
		Reads(PasswordResetRequestBody{}).
		Returns(http.StatusBadRequest, BodyReadFailure, nil).
		Returns(http.StatusBadRequest, BadCaptcha, nil).
		Returns(http.StatusUnauthorized, BadCredentials, nil).
		Writes(true).
		Returns(http.StatusOK, "Reset Code Sent", nil))

	userService.Route(userService.
		POST("/{userName}/PasswordReset").
		To(aService.resetPassword).
		// Docs
		Doc("Attempts to change the user's password using a token").
		Operation("passwordReset").
		Param(userService.PathParameter("userName",
			"The name that identifies a user to our service").DataType("string")).
		Reads(PasswordResetBody{}).
		Returns(http.StatusBadRequest, BodyReadFailure, nil).
		Returns(http.StatusBadRequest, BadCaptcha, nil).
		Returns(http.StatusUnauthorized, BadCredentials, nil).
		Writes("A Valid Session Key").
		Returns(http.StatusOK, "Successfully reset", nil))


	aService.Service = userService

	return nil
}