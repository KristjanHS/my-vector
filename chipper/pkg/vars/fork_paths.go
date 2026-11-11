package vars

import "os"

// SecretsDir holds wire-pod's private state (apiConfig, fork settings, jdocs, session certs, TLS certs); relative to cwd chipper/.
const SecretsDir = "../secrets/server"

// Web-UI auth (myvector/webauth): store, generated password, scripts' `curl -H @` header line.
var (
	WebAuthPath       = SecretsDir + "/webauth.json"
	WebPasswordPath   = "../secrets/webui-password"
	WebAuthHeaderPath = "../secrets/wirepod-auth.header"
)

func init() {
	if Packaged { // Init joins these onto UserConfigDir: keep upstream's relative names there
		return
	}
	ApiConfigPath = SecretsDir + "/apiConfig.json" // fork_lang/fork_prompt and voice.SettingsPath put the myvector*.json files beside it
	BotConfigsPath = SecretsDir + "/botConfig.json"
	JdocsDir = SecretsDir + "/jdocs"
	JdocsPath = JdocsDir + "/jdocs.json"
	BotInfoPath = JdocsDir + "/" + BotInfoName
	SessionCertPath = SecretsDir + "/session-certs/"
	Certs = SecretsDir + "/certs"
	CertPath = Certs + "/cert.crt"
	KeyPath = Certs + "/cert.key"
	ServerConfigPath = Certs + "/server_config.json"
	if _, err := os.Stat("../secrets"); err == nil { // a checkout's chipper/, not a go test cwd: upstream writes into these dirs without creating them
		for _, d := range []string{JdocsDir, SessionCertPath, Certs} {
			os.MkdirAll(d, 0o700)
		}
	}
}
