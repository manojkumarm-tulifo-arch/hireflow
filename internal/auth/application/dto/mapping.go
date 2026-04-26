package dto

import "github.com/hustle/hireflow/internal/auth/domain/entities"

// UserDTOFromEntity maps a User aggregate to its public DTO.
func UserDTOFromEntity(u *entities.User) UserDTO {
	return UserDTO{
		ID:       u.ID().String(),
		TenantID: u.TenantID().String(),
		Email:    u.Email().String(),
		Name:     u.Name(),
		Status:   u.Status().String(),
		Roles:    u.Roles(),
	}
}
