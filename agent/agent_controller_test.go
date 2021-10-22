package agent

import "testing"

func TestGetSessionUsers(t *testing.T) {
	c := &AgentController{}

	// 29 = 6 + 6 + 6 + 6 + 5
	t.Run("29 / 5", func(t *testing.T) {
		if users := c.getSessionUsers(&JobPhase{UsersPerSecond: 29}, 1, 5, 0); users != 6 {
			t.Fatal("29: Major should be 6 but", users)
		}
		if users := c.getSessionUsers(&JobPhase{UsersPerSecond: 29}, 1, 5, 4); users != 5 {
			t.Fatal("29: Last should be 5 but", users)
		}
	})

	// 30 = 6 + 6 + 6 + 6 + 6
	t.Run("30 / 5", func(t *testing.T) {
		if users := c.getSessionUsers(&JobPhase{UsersPerSecond: 30}, 1, 5, 0); users != 6 {
			t.Fatal("30: Major should be 6 but", users)
		}
		if users := c.getSessionUsers(&JobPhase{UsersPerSecond: 30}, 1, 5, 4); users != 6 {
			t.Fatal("30: Last should be 6 but", users)
		}
	})

	// 31 = 7 + 7 + 7 + 7 + 3
	t.Run("31 / 5", func(t *testing.T) {
		if users := c.getSessionUsers(&JobPhase{UsersPerSecond: 31}, 1, 5, 0); users != 7 {
			t.Fatal("31: Major should be 7 but", users)
		}
		if users := c.getSessionUsers(&JobPhase{UsersPerSecond: 31}, 1, 5, 4); users != 3 {
			t.Fatal("31: Last should be 7 but", users)
		}
	})

	// 20 = 3 + 3 + 3 + 3 + 3 + 3 + 2 + 0
	t.Run("20 / 8", func(t *testing.T) {
		if users := c.getSessionUsers(&JobPhase{UsersPerSecond: 20}, 1, 8, 0); users != 3 {
			t.Fatal("20: Major should be 3 but", users)
		}
		if users := c.getSessionUsers(&JobPhase{UsersPerSecond: 20}, 1, 8, 7); users != 0 {
			t.Fatal("20: Last should be 0 but", users)
		}
		if users := c.getSessionUsers(&JobPhase{UsersPerSecond: 20}, 1, 8, 6); users != 2 {
			t.Fatal("20: Last but one should be 2 but", users)
		}
	})
}
