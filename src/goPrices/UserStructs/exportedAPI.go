package UserStructs

//this is where all exported functions meant for direct api usage are kept
//
//the manager manages users, the caller has no responsibility
//except calling the functions and where specifically noted.

import (
	"fmt"
)

const PublicSessionKey string = "public"

const StandardCollectionCount int = 3

//we limit the size of user names, passwords, and emails to these many
//characters to avoid potentially nasty errors
const fieldLength int = 200


//creates a user if they don't already exist, returns an error if they do or
//if we failed to commit them. Returns a usable session key if
//this encounters no errors.
//
//they are brought into the in-memory aManager.users map automatically.
func (aManager *UserManager) AddUser(name, email, password string,
	maxCollections int) (string, error) {

	if len(name)>fieldLength || len(email)>fieldLength ||
	 len(password) > fieldLength {
		return "",
		fmt.Errorf("Name, email, or password too long. Max length is ",fieldLength)	
	}

	//before we do anything else, we ensure that their password is matching
	//the requirements we have
	if !passwordMeetsRequirements(password) {
		return "", fmt.Errorf("Password doesn't meet minimum requirements")
	}

	// Check if the user already exists
	if aManager.userExists(name) {
		return "", fmt.Errorf("User already exists")
	}

	// Check if the email is already in use
	if aManager.emailUsed(email){
		return "", fmt.Errorf("Email already used.")
	}

	nonce, passwordHash, err := passwordDerivation(password)

	firstSession := newSession()

	aFreshUser := User{

		Name:  name,
		Email: email,

		HashedPass: passwordHash,
		Nonce:      nonce,

		Collections: make(map[string]*Collection),

		MaxCollections: maxCollections,

		Sessions: make(map[string]*UserSession),
	}

	//setup the first session for this user
	aFreshUser.Sessions[firstSession.Key] = &firstSession

	// setup the user in the manager's memory
	aManager.userLock.Lock()
	aManager.users[name] = &aFreshUser
	aManager.userLock.Unlock()

	// Reserve an email for the user
	aManager.emailLock.Lock()
	aManager.EmailsToUserNames[email] = name
	aManager.emailLock.Unlock()

	//write the user to persistent storage
	err = aManager.userToStorage(name)
	if err != nil {
		return "", err
	}

	//record the user's name for bookkeeping purposes
	aManager.nameLock.Lock()
	defer aManager.nameLock.Unlock()
	aManager.UserNames = append(aManager.UserNames, name)

	return firstSession.Key, nil

}

//attempts to add a new, blank collection to a named user authenticated with
//the given session
//
//will not overwrite existing collections
func (aManager *UserManager) NewCollection(name, collName, session string) error {
	//find if user is authorized for this
	aUser, err := aManager.authenticateUser(name, session)
	if err != nil {
		return err
	}

	err = aUser.addCollection(collName)
	if err == nil {
		//save the user to storage if the operation was successful
		aManager.userModified(name)
	}

	return err

}

// Attempts to latch a password reset token onto a named user
//
// Contacts a user via their provided email with a reset token.
func (aManager *UserManager) GetPasswordResetToken(name string) error {
	// Get the user if they exist
	aUser, err := aManager.getUser(name)
	if err != nil {
		return fmt.Errorf("Invalid password reset token")
	}

	// Set a new reset token
	err = aUser.getResetToken()
	if err != nil {
		return fmt.Errorf("Failed to acquire reset token")
	}
	// Send the new reset token
	err = aManager.sendPasswordResetMail(aUser.Name,
		aUser.Email,
		aUser.PasswordResetToken.Key)
	if err != nil {
		return fmt.Errorf("Failed to send reset email")
	}

	// Store the user persistently
	err = aManager.userModified(name)
	if err != nil {
		return fmt.Errorf("Failed to store new user in backend")
	}

	return nil

}

//if the token matches to the provided user, their password is set to the
//provided password.
//
//Has the side effect of nuking all sessions associated with the user to
//prevent continued access in the event of the last password being compromised
//
//returns the only valid session key for that user in the event of success
//otherwise, returns an error.
func (aManager *UserManager) ChangePassword(name,
	resetToken, newPassword string) (sessionKey string, err error) {
	//get the user if they exist
	aUser, err := aManager.getUser(name)
	if err != nil {
		return "", fmt.Errorf("Invalid password reset token")
	}

	freshSession, err := aUser.changePassword(resetToken, newPassword)
	if err != nil {
		return "", fmt.Errorf("Invalid password reset token")
	}

	err = aManager.userModified(name)
	if err != nil {
		return "", fmt.Errorf("Failed to store new user in backend")
	}

	return freshSession, nil
}

//attempts to add a trade, which has been checked to contain only
//legitimate magic cards, to a named user's collection authenticated
//with the given session
//
//Caller Responsibility: Ensure contents of trade are ONLY of valid cards
func (aManager *UserManager) AddTrade(name, collName, sessionKey string,
	aTrade Trade) error {

	public := false

	aUser, err := aManager.getUserWithAuthentication(name, sessionKey, public)
	if err != nil {
		return err
	}

	err = aUser.addTrade(collName, aTrade)
	if err != nil {
		return err
	}

	err = aManager.userToStorage(name)

	return err

}

//requests a valid session handle for the named user authenticated with the
//provided password.
func (aManager *UserManager) GetNewSession(name, password string) (string, error) {
	//get the user if they exist
	aUser, err := aManager.getUser(name)
	if err != nil {
		return "", fmt.Errorf("Invalid user")
	}

	// Ensure the provided password is the user's
	candidateHash, err:= derivePasswordRaw([]byte(password), aUser.Nonce)
	if err!=nil || !hashesEqual(aUser.HashedPass, candidateHash) {
		return "", fmt.Errorf("Invalid password")
	}

	//acquire a session
	freshSessionKey := aUser.getNewSession()

	return freshSessionKey, nil

}

func (aManager *UserManager) GetCollectionList(name,
	sessionKey string) ([]string, error) {
	
	public:= sessionKey == PublicSessionKey

	aUser, err := aManager.getUserWithAuthentication(name, sessionKey, public)
	if err != nil {
		return nil, err
	}

	return aUser.getCollectionList(public)

}

//requests the named collection for the named user authenticated
//with the provided session.
//
//a session key of PublicSessionKey attempts to get the collection using public
//permissions as determined by the specific collection the user has
//
//returns the collection, as marshalled to json, and nil if successful
//return nil and an error if failed.
func (aManager *UserManager) GetCollection(name, collName,
	sessionKey string) (*Collection, error) {

	public := sessionKey == PublicSessionKey

	aUser, err := aManager.getUserWithAuthentication(name, sessionKey, public)
	if err != nil {
		return nil, err
	}

	//get the collection, its stripped to public if required
	aColl, err := aUser.getCollection(collName, public)
	if err != nil {
		return nil, err
	}

	return aColl, nil

}

func (aManager *UserManager) SetPermissions(name, collName, sessionKey string,
	viewing, history, comments bool) error {
	
	// Find if the remote user is authorized for this change
	aUser, err := aManager.authenticateUser(name, sessionKey)
	if err != nil {
		return err
	}

	err = aUser.setPermissions(collName, viewing, history, comments)
	if err!=nil {
		return err
	}

	return aManager.userToStorage(name)

}