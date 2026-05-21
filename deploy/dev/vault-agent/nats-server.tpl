{{ with secret "pki_int/issue/ruby-core-nats-server" "common_name=nats" "ip_sans=127.0.0.1" "alt_names=ruby-core-dev-nats,ruby-core-staging-nats,ruby-core-prod-nats,localhost" "ttl=720h" }}{{ .Data.certificate }}
==CA==
{{ .Data.issuing_ca }}
==KEY==
{{ .Data.private_key }}
{{ end }}
