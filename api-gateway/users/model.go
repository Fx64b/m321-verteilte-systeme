package users

import (
	"context"
	"encoding/json"
	"errors"
	"golang.org/x/crypto/bcrypt"
	"time"

	"github.com/go-redis/redis/v8"
)

var (
	ErrUserNotFound       = errors.New("user not found")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUserAlreadyExists  = errors.New("user already exists")
)

// User represents a user in the system (API representation)
type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	Password     string    `json:"-"` // Never return passwords in JSON
	PasswordHash string    `json:"-"` // Never return password hash in JSON
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type userStorage struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"password_hash"`
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type UserStore struct {
	redisClient *redis.Client
}

func NewUserStore(redisClient *redis.Client) *UserStore {
	return &UserStore{
		redisClient: redisClient,
	}
}

func (s *UserStore) Create(ctx context.Context, user *User) error {
	// Check if user already exists
	exists, err := s.redisClient.Exists(ctx, "user:email:"+user.Email).Result()
	if err != nil {
		return err
	}
	if exists == 1 {
		return ErrUserAlreadyExists
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(user.Password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	user.PasswordHash = string(hashedPassword)
	user.Password = "" // Clear plaintext password

	storageUser := userStorage{
		ID:           user.ID,
		Email:        user.Email,
		PasswordHash: user.PasswordHash,
		Role:         user.Role,
		CreatedAt:    user.CreatedAt,
		UpdatedAt:    user.UpdatedAt,
	}

	// Save user to Redis
	userJSON, err := json.Marshal(storageUser)
	if err != nil {
		return err
	}

	// Use a transaction to ensure both operations complete
	pipe := s.redisClient.Pipeline()
	pipe.Set(ctx, "user:"+user.ID, userJSON, 0) // 0 = no expiration
	pipe.Set(ctx, "user:email:"+user.Email, user.ID, 0)
	_, err = pipe.Exec(ctx)
	return err
}

// GetByEmail retrieves a user by email
func (s *UserStore) GetByEmail(ctx context.Context, email string) (*User, error) {
	userID, err := s.redisClient.Get(ctx, "user:email:"+email).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	return s.GetByID(ctx, userID)
}

func (s *UserStore) GetByID(ctx context.Context, id string) (*User, error) {
	userJSON, err := s.redisClient.Get(ctx, "user:"+id).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	var storageUser userStorage
	err = json.Unmarshal([]byte(userJSON), &storageUser)
	if err != nil {
		return nil, err
	}

	user := &User{
		ID:           storageUser.ID,
		Email:        storageUser.Email,
		PasswordHash: storageUser.PasswordHash,
		Role:         storageUser.Role,
		CreatedAt:    storageUser.CreatedAt,
		UpdatedAt:    storageUser.UpdatedAt,
	}

	return user, nil
}

func (s *UserStore) Authenticate(ctx context.Context, email, password string) (*User, error) {
	user, err := s.GetByEmail(ctx, email)
	if err != nil {
		return nil, err
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	return user, nil
}
