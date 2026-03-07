package sites

import (
	"fmt"
	"strings"
)

// GenerateNginxVhost creates an Nginx server block config for a PHP site
func GenerateNginxVhost(domain, docRoot, phpVersion string, port int) string {
	phpSocket := fmt.Sprintf("/var/run/php/php%s-fpm.sock", phpVersion)

	conf := fmt.Sprintf("# TunnelPanel managed - %s\n", domain)
	conf += "# Do not edit manually, changes will be overwritten\n\n"
	conf += "server {\n"
	conf += fmt.Sprintf("    listen %d;\n", port)
	conf += fmt.Sprintf("    server_name %s;\n", domain)
	conf += fmt.Sprintf("    root %s;\n", docRoot)
	conf += "    index index.php index.html index.htm;\n\n"
	conf += fmt.Sprintf("    access_log /var/log/nginx/%s-access.log;\n", domain)
	conf += fmt.Sprintf("    error_log  /var/log/nginx/%s-error.log;\n\n", domain)
	conf += "    add_header X-Frame-Options \"SAMEORIGIN\" always;\n"
	conf += "    add_header X-Content-Type-Options \"nosniff\" always;\n"
	conf += "    add_header X-XSS-Protection \"1; mode=block\" always;\n\n"
	conf += "    client_max_body_size 100M;\n\n"
	conf += "    location / {\n"
	conf += "        try_files $uri $uri/ /index.php?$query_string;\n"
	conf += "    }\n\n"
	conf += "    location ~ \\.php$ {\n"
	conf += fmt.Sprintf("        fastcgi_pass unix:%s;\n", phpSocket)
	conf += "        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;\n"
	conf += "        include fastcgi_params;\n"
	conf += "        fastcgi_index index.php;\n"
	conf += "        fastcgi_read_timeout 300;\n"
	conf += "    }\n\n"
	conf += "    location ~ /\\.(?!well-known) {\n"
	conf += "        deny all;\n"
	conf += "    }\n\n"
	conf += "    location ~* \\.(jpg|jpeg|png|gif|ico|css|js|woff2?|ttf|svg)$ {\n"
	conf += "        expires 30d;\n"
	conf += "        add_header Cache-Control \"public, immutable\";\n"
	conf += "    }\n"
	conf += "}\n"

	// Ensure Unix line endings
	conf = strings.ReplaceAll(conf, "\r\n", "\n")
	conf = strings.ReplaceAll(conf, "\r", "\n")

	return conf
}

// GenerateNginxProxy creates an Nginx reverse proxy config (for containers, Node apps, etc.)
func GenerateNginxProxy(domain string, targetPort, listenPort int) string {
	conf := fmt.Sprintf("# TunnelPanel managed - %s (proxy)\n\n", domain)
	conf += "server {\n"
	conf += fmt.Sprintf("    listen %d;\n", listenPort)
	conf += fmt.Sprintf("    server_name %s;\n\n", domain)
	conf += "    location / {\n"
	conf += fmt.Sprintf("        proxy_pass http://127.0.0.1:%d;\n", targetPort)
	conf += "        proxy_http_version 1.1;\n"
	conf += "        proxy_set_header Upgrade $http_upgrade;\n"
	conf += "        proxy_set_header Connection \"upgrade\";\n"
	conf += "        proxy_set_header Host $host;\n"
	conf += "        proxy_set_header X-Real-IP $remote_addr;\n"
	conf += "        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n"
	conf += "        proxy_set_header X-Forwarded-Proto $scheme;\n"
	conf += "        proxy_read_timeout 86400;\n"
	conf += "    }\n"
	conf += "}\n"

	conf = strings.ReplaceAll(conf, "\r\n", "\n")
	conf = strings.ReplaceAll(conf, "\r", "\n")

	return conf
}
