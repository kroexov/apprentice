package rpc

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"regexp"
	"strings"
	"unicode"

	"apisrv/pkg/db"

	"github.com/go-pg/pg/v10"
	"github.com/vmkteam/embedlog"
	"github.com/vmkteam/zenrpc/v2"
	"golang.org/x/crypto/bcrypt"
)

// User type discriminator for AuthService.Login / AuthService.Register.
const (
	UserTypeAdmin = "admin"
	UserTypeUser  = "user"
)

// authLoginRegex matches the candidates_login_check / candidates_handle_check
// CHECK in docs/apisrv.sql so registered logins survive both DB constraints.
var authLoginRegex = regexp.MustCompile(`^[a-z0-9.\-_]{2,40}$`)

// bcrypt's input ceiling is 72 bytes — anything longer is silently truncated.
const (
	passwordMinLen = 8
	passwordMaxLen = 72
	authKeyLength  = 32
)

type AuthService struct {
	zenrpc.Service
	embedlog.Logger

	dbo        db.DB
	commonRepo db.CommonRepo
	repo       db.ApprenticeRepo
}

func NewAuthService(dbo db.DB, logger embedlog.Logger) *AuthService {
	return &AuthService{
		dbo:        dbo,
		commonRepo: db.NewCommonRepo(dbo),
		repo:       db.NewApprenticeRepo(dbo),
		Logger:     logger,
	}
}

// Login authenticates an existing admin or candidate and returns a fresh authKey.
//
//zenrpc:login User login
//zenrpc:password User password
//zenrpc:userType User type ("admin" or "user")
//zenrpc:return User authentication key
//zenrpc:400 Validation Error
//zenrpc:500 Internal Error
func (s AuthService) Login(ctx context.Context, login, password, userType string) (string, error) {
	if err := validateLoginCredentials(login, password); err != nil {
		return "", err
	}
	switch userType {
	case UserTypeAdmin:
		return s.loginAdmin(ctx, login, password)
	case UserTypeUser:
		return s.loginUser(ctx, login, password)
	default:
		return "", ErrInvalidUserType
	}
}

// Register creates a new candidate and returns a fresh authKey. Only
// userType="user" is accepted — admin self-registration is intentionally not
// available over RPC. Admins are seeded via init.sql or created by another
// admin through CandidateService.Add.
//
//zenrpc:login User login
//zenrpc:password User password
//zenrpc:userType User type ("user" only)
//zenrpc:return User authentication key
//zenrpc:400 Validation Error
//zenrpc:500 Internal Error
func (s AuthService) Register(ctx context.Context, login, password, userType string) (string, error) {
	if err := validateRegisterCredentials(login, password); err != nil {
		return "", err
	}
	if userType != UserTypeUser {
		return "", ErrInvalidUserType
	}
	return s.registerUser(ctx, login, password)
}

// dummyBcryptHash is a precomputed bcrypt hash used to keep timing of
// failed-login responses comparable between "user not found" and "user found
// but wrong password" paths. Without this, response time leaks user existence.
// Generated once via passwordHash("dummy") at init.
var dummyBcryptHash = mustDummyHash()

func mustDummyHash() string {
	h, err := passwordHash("dummy-password")
	if err != nil {
		panic("rpc/auth: bcrypt failure: " + err.Error())
	}
	return h
}

func (s AuthService) loginAdmin(ctx context.Context, login, password string) (string, error) {
	dbu, err := s.commonRepo.EnabledUserByLogin(ctx, login)
	if err != nil {
		return "", InternalError(err)
	}
	if dbu == nil {
		// Anti-enumeration: spend the same CPU as a real bcrypt check would.
		_ = checkHash(password, dummyBcryptHash)
		return "", ErrInvalidLoginPassword
	}
	if !checkHash(password, dbu.Password) {
		return "", ErrInvalidLoginPassword
	}
	if ok, err := s.commonRepo.AuthenticateUser(ctx, dbu, generateAuthKey()); err != nil || !ok {
		return "", InternalError(err)
	}
	s.Print(ctx, "admin login", "userId", dbu.ID)
	return dbu.AuthKey, nil
}

func (s AuthService) loginUser(ctx context.Context, login, password string) (string, error) {
	dbc, err := s.repo.EnabledCandidateByLogin(ctx, login)
	if err != nil {
		return "", InternalError(err)
	}
	if dbc == nil {
		_ = checkHash(password, dummyBcryptHash)
		return "", ErrInvalidLoginPassword
	}
	if !checkHash(password, dbc.Password) {
		return "", ErrInvalidLoginPassword
	}
	if ok, err := s.repo.AuthenticateCandidate(ctx, dbc, generateAuthKey()); err != nil || !ok {
		return "", InternalError(err)
	}
	s.Print(ctx, "candidate login", "candidateId", dbc.ID)
	return dbc.AuthKey, nil
}

// registerUser creates a candidate inside an advisory-locked transaction so
// concurrent Register(login=X, ...) calls do not race past the pre-check and
// surface as 500s — the second one observes the first and returns ErrLoginTaken.
func (s AuthService) registerUser(ctx context.Context, login, password string) (string, error) {
	hash, err := passwordHash(password)
	if err != nil {
		return "", InternalError(err)
	}

	var authKey string
	lockName := "rpc-auth-register-" + login
	err = s.dbo.RunInLock(ctx, lockName, func(tx *pg.Tx) error {
		txRepo := s.repo.WithTransaction(tx)

		existing, txErr := txRepo.OneCandidate(ctx, &db.CandidateSearch{Login: &login})
		if txErr != nil {
			return txErr
		}
		if existing != nil {
			return ErrLoginTaken
		}

		stage, txErr := txRepo.NextStageAfter(ctx, 0)
		if txErr != nil {
			return txErr
		}
		if stage == nil {
			return ErrNoStagesAvailable
		}

		cand := &db.Candidate{
			Name:           login,
			Handle:         login,
			Login:          login,
			Password:       hash,
			Initials:       defaultInitials(login),
			Strengths:      []string{},
			Weaknesses:     []string{},
			CurrentStageID: stage.ID,
			StatusID:       db.StatusEnabled,
		}
		created, txErr := txRepo.AddCandidate(ctx, cand)
		if txErr != nil {
			if db.IsUniqueViolation(txErr) {
				return ErrLoginTaken
			}
			return txErr
		}
		if _, txErr = txRepo.CreateCandidateStage(ctx, created.ID, stage); txErr != nil {
			return txErr
		}
		authKey = generateAuthKey()
		if _, txErr = txRepo.AuthenticateCandidate(ctx, created, authKey); txErr != nil {
			return txErr
		}
		s.Print(ctx, "candidate registered", "candidateId", created.ID, "stageId", stage.ID)
		return nil
	})
	if err != nil {
		if mapped := mapAuthErr(err); mapped != nil {
			return "", mapped
		}
		return "", InternalError(err)
	}
	return authKey, nil
}

// mapAuthErr lifts a typed *zenrpc.Error returned from inside a transaction
// back to the caller, so RunInLock failures don't get wrapped as 500s.
func mapAuthErr(err error) *zenrpc.Error {
	var ze *zenrpc.Error
	if errors.As(err, &ze) {
		return ze
	}
	return nil
}

// validateLoginCredentials only rejects empty login/password — anything else
// (login format, password length) is left to the bcrypt path so we don't
// (a) lock out users whose passwords predate the current policy, and
// (b) leak format hints via fast-path responses on bad logins.
func validateLoginCredentials(login, password string) error {
	if login == "" || password == "" {
		return ErrInvalidLoginPassword
	}
	return nil
}

// validateRegisterCredentials enforces full format + policy on new accounts:
// login matches authLoginRegex, password is within passwordMinLen..passwordMaxLen.
func validateRegisterCredentials(login, password string) error {
	if login == "" || password == "" {
		return ErrInvalidLoginPassword
	}
	if !authLoginRegex.MatchString(login) {
		return ErrInvalidLoginPassword
	}
	if len(password) < passwordMinLen || len(password) > passwordMaxLen {
		return ErrPasswordPolicy
	}
	return nil
}

// bcryptCost is the work factor for bcrypt. Production = 14 (~800ms/hash on
// modern hardware) for password-storage strength. Tests override it to
// bcrypt.MinCost (4, ~3ms) via an init() in *_test.go to keep the suite fast —
// anything that asserts timing is responsible for restoring/forcing a value.
var bcryptCost = 14

func passwordHash(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func checkHash(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// generateAuthKey returns a 32-char URL-safe base64 token from crypto/rand.
// 192 bits of entropy (24 random bytes) — predictability resistance for an
// authentication token. Panics if the OS RNG fails (which would compromise
// the security model entirely).
func generateAuthKey() string {
	b := make([]byte, 24) // 24 bytes → 32 base64url chars
	if _, err := rand.Read(b); err != nil {
		panic("rpc/auth: crypto/rand failure: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// defaultInitials picks the first 1–2 alphanumeric runes of the login,
// uppercased. Used when registering a candidate via Register where the caller
// only provides credentials. Never returns an empty string for valid logins
// because authLoginRegex enforces at least 2 chars.
func defaultInitials(login string) string {
	var (
		runes  []rune
		cursor int
	)
	for _, r := range strings.ToUpper(login) {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			continue
		}
		runes = append(runes, r)
		cursor++
		if cursor == 2 {
			break
		}
	}
	if len(runes) == 0 {
		return "X"
	}
	return string(runes)
}
