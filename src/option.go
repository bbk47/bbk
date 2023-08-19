package bbk

type TunnelOpts struct {
	Protocol string `json:"protocol"`
	Secure   bool   `json:"secure"`
	Host     string `json:"host"`
	Port     string `json:"port"`
	Path     string `json:"path"`
	Method   string `json:"method"`
	Password string `json:"password"`
}

type ClientOpts struct {
	Mode           string      `json:"mode"`
	ListenAddr     string      `json:"listenAddr"`
	ListenPort     int         `json:"listenPort"`
	ListenHttpPort int         `json:"listenPort"`
	LogLevel       string      `json:"logLevel"`
	Ping           bool        `json:"ping"`
	TunnelOpts     *TunnelOpts `json:"tunnelOpts"`
}

type ServerOpts struct {
	Mode       string `json:"mode"`
	ListenAddr string `json:"listenAddr"`
	ListenPort int    `json:"listenPort"`
	LogLevel   string `json:"logLevel"`
	Password   string `json:"password"`
	Method     string `json:"method"`
	WorkMode   string `json:"workMode"`
	WorkPath   string `json:"workPath"`
	SslKey     string `json:"sslKey"` // server
	SslCrt     string `json:"sslCrt"` // server
}

//const defaultOpts = {
//	mode: 'server',
//	method: 'aes-256-cfb', // local/server
//	password: 'p@ssword', // local/server
//	listenAddr: '127.0.0.1', // local/server
//	listenPort: 5900, // local/server
//	logLevel: 'info', // local/server
//	workMode: 'ws',
//	workPath: '/wss',
//	tunnelOpts: {
//		protocol: 'ws',
//		secure: false,
//		host: '127.0.0.1',
//		port: 5900,
//		path: '/wss',
//		method: 'aes-256-cfb',
//		password: 'p@ssword',
//	},
//	ping: false,
//};
