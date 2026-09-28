package model

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"
)

// ValidateMigrationUsername 校验显式指定的迁移用户名，不修改用户提供的名称。
// @param username 目标用户的原始名称。
// @return 名称非法时返回错误。
func ValidateMigrationUsername(username string) error {
	if username == "" || strings.TrimSpace(username) != username || !utf8.ValidString(username) {
		return errors.New("用户名不能为空、包含无效编码或首尾空白")
	}
	if utf8.RuneCountInString(username) > UserNameMaxLength {
		return errors.New("用户名超过 64 个 Unicode 码点")
	}
	for _, r := range username {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return errors.New("用户名包含控制或不可见字符")
		}
	}
	return nil
}

// BuildExplicitMigratedUsername 在迁移事务内确认显式用户名尚未被占用。
// @param tx 当前迁移事务。
// @param username 经过用户确认的完整用户名。
// @param assigned 本批次已成功迁移的用户名集合。
// @return 原样返回可用用户名；冲突时返回错误，不截断或添加后缀。
func BuildExplicitMigratedUsername(tx *gorm.DB, username string, assigned map[string]bool) (string, error) {
	if tx == nil || assigned == nil {
		return "", errors.New("迁移事务或用户名集合不可用")
	}
	if err := ValidateMigrationUsername(username); err != nil {
		return "", err
	}
	if assigned[username] {
		return "", errors.New("用户名在本批次已被占用")
	}
	exists, err := usernameExistsInDB(tx, username)
	if err != nil {
		return "", err
	}
	if exists {
		return "", errors.New("用户名已存在")
	}
	return username, nil
}
