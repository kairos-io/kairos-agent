package boards

import (
	"github.com/joho/godotenv"
	"os"

	cnst "github.com/kairos-io/kairos-agent/v2/pkg/constants"
)

// IsAnAndroidBoard returns true if the system is an Android board
// based on checking if there is a build.prop file
func IsAnAndroidBoard() bool {
	// Check if we are running on an Android board
	_, err := os.Stat("/build.prop")
	if err == nil {
		return true
	}
	return false
}

// GetAndroidBoardModel returns the board model if the system is an Android board
func GetAndroidBoardModel() string {
	// Check if we are running on an Android board
	if IsAnAndroidBoard() {
		buildProp, err := godotenv.Read("/build.prop")
		if err != nil {
			return ""
		}
		switch buildProp["ro.product.board"] {
		case cnst.QCS6490:
			return cnst.QCS6490
		default:
			return ""
		}

	}

	return ""
}
