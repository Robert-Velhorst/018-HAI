package autoconfig

const configTemplate = `location /{{.URLPath}}/ {
	set $upstream_host {{.Host}};
    set $upstream_port {{.Port}};
    auth_request /auth-verify;
    auth_request_set $hai_refreshed_access_cookie $upstream_http_x_hai_refreshed_access_cookie;
    auth_request_set $hai_refreshed_refresh_cookie $upstream_http_x_hai_refreshed_refresh_cookie;
    add_header Set-Cookie $hai_refreshed_access_cookie always;
    add_header Set-Cookie $hai_refreshed_refresh_cookie always;
    proxy_pass http://$upstream_host:$upstream_port;
}
`
