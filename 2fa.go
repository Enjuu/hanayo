package main

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	"git.zxq.co/ripple/rippleapi/common"
	"git.zxq.co/x/rs"

	"github.com/gin-gonic/contrib/sessions"
	"github.com/gin-gonic/gin"
)

var allowedPaths = [...]string{
	"/logout",
	"/2fa_gateway",
	"/2fa_gateway/verify",
	"/2fa_gateway/clear",
}

// middleware to deny all requests to non-allowed pages
func twoFALock(c *gin.Context) {
	sess := c.MustGet("session").(sessions.Session)
	if v, _ := sess.Get("2fa_must_validate").(bool); !v {
		c.Next()
		return
	}

	// check it's not a static file
	if len(c.Request.URL.Path) >= 8 && c.Request.URL.Path[:8] == "/static/" {
		c.Next()
		return
	}

	// check it's one of the few approved paths
	for _, a := range allowedPaths {
		if a == c.Request.URL.Path {
			c.Next()
			return
		}
	}
	addMessage(c, warningMessage{"You need to complete the 2fa challenge first."})
	c.Redirect(302, "/2fa_gateway")
	c.Abort()
}

// is2faEnabled checks 2fa is enabled for an user.
func is2faEnabled(user int) bool {
	return db.QueryRow("SELECT 1 FROM 2fa_telegram WHERE userid = ?", user).Scan(new(int)) != sql.ErrNoRows
}

func tfaGateway(c *gin.Context) {
	sess := c.MustGet("session").(sessions.Session)

	// check 2fa hasn't been disabled
	i, _ := sess.Get("userid").(int)
	if i == 0 {
		c.Redirect(302, "/")
	}
	if !is2faEnabled(i) {
		sess.Delete("2fa_must_validate")
		c.Redirect(302, "/")
		return
	}
	// check previous 2fa thing is still valid
	err := db.QueryRow("SELECT 1 FROM 2fa WHERE userid = ? AND ip = ? AND expire > ?",
		i, c.ClientIP(), time.Now().Unix()).Scan(new(int))
	if err != nil {
		db.Exec("INSERT INTO 2fa(userid, token, ip, expire, sent) VALUES (?, ?, ?, ?, 0);",
			i, strings.ToUpper(rs.String(8)), c.ClientIP(), time.Now().Add(time.Hour).Unix())
		http.Get("http://127.0.0.1:8888/update")
	}

	resp(c, 200, "2fa_gateway.html", &baseTemplateData{
		TitleBar:  "Two Factor Authentication",
		KyutGrill: "2fa.jpg",
	})
}

func clear2fa(c *gin.Context) {
	// basically deletes from db 2fa tokens, so that it gets regenerated when user hits gateway page
	sess := c.MustGet("session").(sessions.Session)
	i, _ := sess.Get("userid").(int)
	if i == 0 {
		c.Redirect(302, "/")
	}
	db.Exec("DELETE FROM 2fa WHERE userid = ? AND ip = ?", i, c.ClientIP())
	addMessage(c, successMessage{"A new code has been generated and sent to you through Telegram."})
	sess.Save()
	c.Redirect(302, "/2fa_gateway")
}

func verify2fa(c *gin.Context) {
	sess := c.MustGet("session").(sessions.Session)
	i, _ := sess.Get("userid").(int)
	if i == 0 {
		c.Redirect(302, "/")
	}
	var id int
	var expire common.UnixTimestamp
	err := db.QueryRow("SELECT id, expire FROM 2fa WHERE userid = ? AND ip = ? AND token = ?", i, c.ClientIP(), strings.ToUpper(c.Query("token"))).Scan(&id, &expire)
	if err == sql.ErrNoRows {
		c.String(200, "1")
		return
	}
	if time.Now().After(time.Time(expire)) {
		c.String(200, "1")
		db.Exec("INSERT INTO 2fa(userid, token, ip, expire, sent) VALUES (?, ?, ?, ?, 0);",
			i, strings.ToUpper(rs.String(8)), c.ClientIP(), time.Now().Add(time.Hour).Unix())
		http.Get("http://127.0.0.1:8888/update")
		return
	}
	logIP(c, i)
	addMessage(c, successMessage{"You've been successfully logged in."})
	sess.Delete("2fa_must_validate")
	sess.Save()
	db.Exec("DELETE FROM 2fa WHERE id = ?", id)
	c.String(200, "0")
}