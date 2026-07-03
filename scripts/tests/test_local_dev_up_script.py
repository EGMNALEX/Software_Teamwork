import os
import shutil
import stat
import subprocess
import textwrap
import tempfile
import unittest
from pathlib import Path


class LocalDevUpScriptTests(unittest.TestCase):
    def test_minio_init_success_continues_to_migrations_and_seed(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = self.prepare_runtime(Path(directory))

            result = self.run_dev_up(root)

            self.assertEqual(0, result.returncode, result.stderr)
            self.assertIn("local dev-up: initializing MinIO buckets succeeded", result.stdout)
            self.assertIn("local dev-up: migrating auth", result.stdout)
            self.assertIn("infra, migrations, and seed are ready", result.stdout)

            docker_calls = (root / "docker-calls.log").read_text(encoding="utf-8")
            health_wait_call = next(line for line in docker_calls.splitlines() if " up -d --wait " in line)
            self.assertIn(" postgres redis qdrant minio", health_wait_call)
            self.assertNotIn("minio-init", health_wait_call)
            self.assertIn(" up --no-deps --exit-code-from minio-init minio-init", docker_calls)

    def test_minio_init_failure_stops_before_migrations_with_log_hint(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = self.prepare_runtime(Path(directory))

            result = self.run_dev_up(root, {"FAKE_MINIO_INIT_EXIT": "7"})

            self.assertNotEqual(0, result.returncode)
            self.assertNotIn("local dev-up: migrating auth", result.stdout)
            self.assertIn(
                "minio-init failed; inspect logs with: docker compose -f deploy/docker-compose.yml --env-file deploy/.env logs minio-init",
                result.stderr,
            )

    def test_missing_psql_fails_before_docker_work(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = self.prepare_runtime(Path(directory))
            (root / "fake-bin" / "psql").unlink()

            result = self.run_dev_up(root, {"PATH": str(root / "fake-bin")})

            self.assertNotEqual(0, result.returncode)
            self.assertIn("missing required local command(s): psql", result.stderr)
            self.assertIn("Install the missing host tool(s), then rerun ./scripts/local/dev-up.sh.", result.stderr)
            self.assertNotIn("Check Docker with:", result.stderr)
            self.assertNotIn("If Go module download failed", result.stderr)
            self.assertIn("local dev-up: checking local tool dependencies", result.stdout)
            self.assertNotIn("local dev-up: pulling infrastructure images", result.stdout)
            self.assertFalse((root / "docker-calls.log").exists())

    def prepare_runtime(self, root: Path) -> Path:
        script_source = Path.cwd() / "scripts" / "local" / "dev-up.sh"
        script_target = root / "scripts" / "local" / "dev-up.sh"
        script_target.parent.mkdir(parents=True)
        shutil.copy2(script_source, script_target)
        script_target.chmod(script_target.stat().st_mode | stat.S_IXUSR)

        (root / "deploy" / "seeds").mkdir(parents=True)
        (root / "deploy" / "docker-compose.yml").write_text("services: {}\n", encoding="utf-8")
        (root / "deploy" / ".env").write_text(
            textwrap.dedent(
                """\
                GOPROXY=https://goproxy.cn,direct
                GOSUMDB=sum.golang.google.cn
                QDRANT_URL=
                AUTH_DATABASE_URL=postgres://example/auth
                FILE_DATABASE_URL=postgres://example/file
                KNOWLEDGE_DATABASE_URL=postgres://example/knowledge
                QA_DATABASE_URL=postgres://example/qa
                DOCUMENT_DATABASE_URL=postgres://example/document
                AI_GATEWAY_DATABASE_URL=postgres://example/ai_gateway
                POSTGRES_ADMIN_URL=postgres://example/postgres
                """
            ),
            encoding="utf-8",
        )
        for seed in [
            "001-local-demo-seed.sql",
            "002-ai-gateway-model-profiles.sql",
            "003-qa-document-mcp.sql",
            "004-qa-default-knowledge-base.sql",
        ]:
            (root / "deploy" / "seeds" / seed).write_text("-- seed\n", encoding="utf-8")
        for service in ["auth", "file", "knowledge", "qa", "document", "ai-gateway"]:
            (root / "services" / service).mkdir(parents=True)

        fake_bin = root / "fake-bin"
        fake_bin.mkdir()
        for command in ["bash", "dirname"]:
            target = shutil.which(command)
            if target is None:
                raise AssertionError(f"{command} is required to run dev-up.sh tests")
            os.symlink(target, fake_bin / command)
        self.write_executable(
            fake_bin / "docker",
            """\
            #!/usr/bin/env bash
            echo "$*" >> "$FAKE_DOCKER_CALLS"
            case " $* " in
              *" up -d --wait "*)
                if [[ " $* " == *" minio-init "* ]]; then
                  echo "minio-init must not be part of the health wait" >&2
                  exit 80
                fi
                ;;
              *" up --no-deps --exit-code-from minio-init minio-init "*)
                exit "${FAKE_MINIO_INIT_EXIT:-0}"
                ;;
            esac
            exit 0
            """,
        )
        self.write_executable(
            fake_bin / "go",
            """\
            #!/usr/bin/env bash
            exit 0
            """,
        )
        self.write_executable(
            fake_bin / "psql",
            """\
            #!/usr/bin/env bash
            exit 0
            """,
        )
        return root

    def run_dev_up(self, root: Path, extra_env: dict[str, str] | None = None) -> subprocess.CompletedProcess[str]:
        env = os.environ.copy()
        env.update(extra_env or {})
        env["FAKE_DOCKER_CALLS"] = str(root / "docker-calls.log")
        if extra_env and "PATH" in extra_env:
            env["PATH"] = extra_env["PATH"]
        else:
            env["PATH"] = f"{root / 'fake-bin'}{os.pathsep}{env['PATH']}"
        return subprocess.run(
            [str(root / "scripts" / "local" / "dev-up.sh")],
            cwd=root,
            env=env,
            text=True,
            capture_output=True,
            check=False,
        )

    def write_executable(self, path: Path, content: str) -> None:
        path.write_text(textwrap.dedent(content), encoding="utf-8")
        path.chmod(path.stat().st_mode | stat.S_IXUSR)


if __name__ == "__main__":
    unittest.main()
