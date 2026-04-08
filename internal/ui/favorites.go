package ui

import (
	"os"
	"path/filepath"
	"strings"
)

const favoritesFile = ".gomposer_favorites"

func favoritesPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, favoritesFile)
}

func LoadFavorites() map[string]bool {
	data, err := os.ReadFile(favoritesPath())
	if err != nil {
		return map[string]bool{}
	}
	favs := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			favs[line] = true
		}
	}
	return favs
}

func SaveFavorites(favs map[string]bool) {
	var lines []string
	for id := range favs {
		lines = append(lines, id)
	}
	os.WriteFile(favoritesPath(), []byte(strings.Join(lines, "\n")+"\n"), 0644) //nolint:errcheck
}
