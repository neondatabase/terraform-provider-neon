terraform {
  required_providers {
    neon = {
      source = "neondatabase/neon"
      # Terraform installs a beta release only when the version is pinned exactly.
      version = "0.19.0-beta.0"
    }
  }
}

provider "neon" {}
