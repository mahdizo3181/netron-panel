package panel

import "github.com/mhsanaei/3x-ui/v3/internal/web/service"

// refuseResellerUsername keeps admin and reseller login names disjoint.
func refuseResellerUsername(username string) error {
	taken, err := service.ResellerUsernameExists(username)
	if err != nil {
		return err
	}
	if taken {
		return service.ErrResellerUsernameTaken
	}
	return nil
}
