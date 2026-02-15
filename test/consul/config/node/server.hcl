datacenter = "dc1"

data_dir = "/consul/data"

bind_addr   = "0.0.0.0"
client_addr = "0.0.0.0"

ui_config {
  enabled = true
  content_path = "/consul/"
}

retry_join = []

tls {
  defaults {
    ca_file   = "/consul/tls/ca/ca.pem"
    cert_file = "/consul/tls/id/consul.pem"
    key_file  = "/consul/tls/id/consul-key.pem"

    verify_incoming = true
    verify_outgoing = true
  }

  internal_rpc {
    verify_server_hostname = false
  }
}

acl {
  enabled                  = false
}

ports {
  grpc     = 8502
  grpc_tls = -1
}

connect {
  enabled = true

  ca_config {
    provider = "consul" 
    
    config { 
      leaf_cert_ttl = "6h" 
      intermediate_cert_ttl = "72h" 
      root_cert_ttl = "87600h"
    }
  }
}
