package store

import "strings"

var avatarPresetKeys = map[string]struct{}{
	"avatar-01": {},
	"avatar-02": {},
	"avatar-03": {},
	"avatar-04": {},
	"avatar-05": {},
	"avatar-06": {},
	"avatar-07": {},
	"avatar-08": {},
	"avatar-09": {},
	"avatar-10": {},
}

func parseAvatarKey(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	key := strings.TrimSpace(*value)
	if key == "" {
		return nil, nil
	}
	if _, ok := avatarPresetKeys[key]; !ok {
		return nil, ErrInvalid
	}
	return &key, nil
}
