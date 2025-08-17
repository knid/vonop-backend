package requests

type UserInfoRequest struct {
	Token string `json:"token" validator:"required"`
}

type UserUpdateRequest struct {
	UserRegisterRequest
}

type UserRegisterRequest struct {
	Name          string `json:"name" validator:"min=3,max=30,required"`
	Email         string `json:"email" validator:"email,required"`
	Photo         string `json:"photo"`
	Password      string `json:"password" validator:"required,min=8,max=64"`
	PasswordCheck string `json:"password_check" validator:"required,min=8,max=64"`
}

type UserLoginRequest struct {
	Email    string `json:"email" validator:"email,required"`
	Password string `json:"password" validator:"required,min=8,max=64"`
}

type UserForgotPasswordRequest struct {
	Email string `json:"email" validator:"email,required"`
}

type UserVerifyEmailRequest struct {
	Email string `json:"email" validator:"email,required"`
	Token string `json:"token" validator:"required"`
}

type UserResendVerifyEmailRequest struct {
	Email string `json:"email" validator:"email,required"`
}
