use anyhow::{Context, Result};
use std::fs;
use std::path::PathBuf;

use crate::api::Config;

pub fn get_config_path() -> Result<PathBuf> {
    let config_dir = dirs::home_dir()
        .context("Failed to get home directory")?
        .join(".cloudcent");
    fs::create_dir_all(&config_dir)
        .context("Failed to create config directory")?;
    Ok(config_dir.join("config.yaml"))
}

pub fn load_config() -> Result<Option<Config>> {
    let config_path = get_config_path()?;
    if !config_path.exists() {
        return Ok(None);
    }
    
    let content = fs::read_to_string(&config_path)
        .context("Failed to read config file")?;
    let config: Config = serde_yaml::from_str(&content)
        .context("Failed to parse config YAML")?;
    Ok(Some(config))
}

pub fn save_config(config: &Config) -> Result<()> {
    let config_path = get_config_path()?;
    
    let content = serde_yaml::to_string(config)
        .context("Failed to serialize config to YAML")?;
    
    fs::write(&config_path, content)
        .context("Failed to write config file")?;
    
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(&config_path)?.permissions();
        perms.set_mode(0o600);
        fs::set_permissions(&config_path, perms)?;
    }
    
    Ok(())
}
