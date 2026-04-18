#[cfg(test)]
mod tests {
    use crate::config::*;

    fn minimal_config() -> Config {
        Config {
            name: "test-app".to_string(),
            stack: None,
            repo: RepoConfig::default(),
            docker: DockerConfig::default(),
            services: ServicesConfig::default(),
            port_offset: PortOffsetConfig::default(),
            database: None,
            redis: None,
            env: EnvConfig::default(),
            features: FeaturesConfig::default(),
            dash: DashConfig::default(),
            git: GitConfig::default(),
            paths: PathsConfig::default(),
            repo_root: std::path::PathBuf::from("/test"),
            config_path: std::path::PathBuf::from("/test/wt.config.js"),
            worktrees_dir_abs: std::path::PathBuf::new(),
            compose_path_abs: None,
            proxy_dynamic_dir: None,
            compose_file_abs: None,
        }
    }

    #[test]
    fn container_name() {
        let cfg = minimal_config();
        assert_eq!(cfg.container_name("my-feat"), "test-app-my-feat");
    }

    #[test]
    fn container_prefix() {
        let cfg = minimal_config();
        assert_eq!(cfg.container_prefix(), "test-app-");
    }

    #[test]
    fn container_filter() {
        let cfg = minimal_config();
        assert_eq!(cfg.container_filter(), "name=test-app-");
    }

    #[test]
    fn volume_prefix() {
        let cfg = minimal_config();
        assert_eq!(cfg.volume_prefix("feat"), "test-app_feat");
    }

    #[test]
    fn domain_for_with_template() {
        let mut cfg = minimal_config();
        cfg.docker.proxy.domain_template = "{alias}.localhost".to_string();
        assert_eq!(cfg.domain_for("my-feat"), Some("my-feat.localhost".to_string()));
    }

    #[test]
    fn domain_for_no_template() {
        let cfg = minimal_config();
        assert_eq!(cfg.domain_for("my-feat"), None);
    }

    #[test]
    fn db_name_with_database() {
        let mut cfg = minimal_config();
        cfg.database = Some(DatabaseConfig {
            db_type: "mongodb".to_string(),
            db_name_prefix: "db_".to_string(),
            ..DatabaseConfig::default()
        });
        assert_eq!(cfg.db_name("my-feat"), Some("db_my_feat".to_string()));
    }

    #[test]
    fn db_name_sanitizes() {
        let mut cfg = minimal_config();
        cfg.database = Some(DatabaseConfig {
            db_type: "mongodb".to_string(),
            db_name_prefix: "db_".to_string(),
            ..DatabaseConfig::default()
        });
        assert_eq!(cfg.db_name("feat/special-chars!@#"), Some("db_feat_special_chars___".to_string()));
    }

    #[test]
    fn db_name_no_database() {
        let cfg = minimal_config();
        assert_eq!(cfg.db_name("feat"), None);
    }

    #[test]
    fn primary_port() {
        let mut cfg = minimal_config();
        cfg.services.primary = "web".to_string();
        cfg.services.ports.insert("web".to_string(), 3000);
        cfg.services.ports.insert("api".to_string(), 4000);
        assert_eq!(cfg.primary_port(), Some(3000));
    }

    #[test]
    fn primary_port_not_set() {
        let cfg = minimal_config();
        assert_eq!(cfg.primary_port(), None);
    }

    #[test]
    fn compute_ports_with_offset() {
        let mut cfg = minimal_config();
        cfg.services.ports.insert("web".to_string(), 3000);
        cfg.services.ports.insert("api".to_string(), 4000);
        let ports = cfg.compute_ports(50);
        assert_eq!(ports["web"], 3050);
        assert_eq!(ports["api"], 4050);
    }

    #[test]
    fn feature_enabled() {
        let mut cfg = minimal_config();
        cfg.features.lan = true;
        cfg.features.prune = false;
        assert!(cfg.feature_enabled("lan"));
        assert!(!cfg.feature_enabled("prune"));
        assert!(!cfg.feature_enabled("nonexistent"));
    }

    #[test]
    fn default_port_offset() {
        let po = PortOffsetConfig::default();
        assert_eq!(po.algorithm, "sha256");
        assert_eq!(po.min, 100);
        assert_eq!(po.range, 2000);
    }

    #[test]
    fn service_manager_default_is_empty() {
        // The dashboard no longer assumes PM2 when a project doesn't
        // declare a runtime — an empty manager means shell + tasks only,
        // no background process probing.
        let cfg = minimal_config();
        assert_eq!(cfg.service_manager(), "");
    }

    #[test]
    fn docker_service_manager_override() {
        let mut cfg = minimal_config();
        cfg.dash.services.docker = Some(DashDockerSvc {
            manager: "static".to_string(),
        });
        assert_eq!(cfg.docker_service_manager(), "static");
    }

    #[test]
    fn docker_service_manager_falls_back_to_top_level() {
        // When no docker-specific manager is set, docker_service_manager
        // falls back to dash.services.manager — which is now empty by
        // default, matching the explicit-opt-in policy.
        let cfg = minimal_config();
        assert_eq!(cfg.docker_service_manager(), "");
    }
}
