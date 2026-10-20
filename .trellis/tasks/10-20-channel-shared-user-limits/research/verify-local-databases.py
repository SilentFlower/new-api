"""使用带专属标签的临时数据库验证三库合同，结束后移除本次创建的容器。"""
from pathlib import Path
import json
import os
import secrets
import subprocess
import time

base = Path(__file__).resolve().parent
project = base.parents[3]
docker = ['docker', '-H', 'unix:///var/run/docker.sock']
run_id = 'newapi-channel-parent-' + secrets.token_hex(4)
created = []
ports = {}
report = {'run_id': run_id, 'images': {'mysql': 'mysql:8.0', 'postgres': 'postgres:16-alpine'}, 'passed': False, 'cleaned': False}
password = secrets.token_hex(16)
try:
    for kind, image, container_port, settings in [
        ('mysql', 'mysql:8.0', '3306', {'MYSQL_ROOT_PASSWORD': password, 'MYSQL_DATABASE': 'codex_parent_tests', 'MYSQL_USER': 'codex_parent', 'MYSQL_PASSWORD': password}),
        ('postgres', 'postgres:16-alpine', '5432', {'POSTGRES_PASSWORD': password, 'POSTGRES_USER': 'codex_parent', 'POSTGRES_DB': 'codex_parent_tests'}),
    ]:
        child_env = os.environ.copy()
        child_env.update(settings)
        args = docker + ['run', '--detach', '--name', run_id + '-' + kind, '--label', 'io.codex.task=' + run_id, '--memory', '512m', '--cpus', '1', '-p', '127.0.0.1::' + container_port]
        for key in settings:
            args += ['-e', key]
        args.append(image)
        result = subprocess.run(args, env=child_env, capture_output=True, text=True)
        if result.returncode:
            raise RuntimeError('启动临时数据库失败：' + result.stderr)
        container_id = result.stdout.strip()
        created.append(container_id)
        ports[kind] = subprocess.check_output(docker + ['port', container_id, container_port + '/tcp'], text=True).strip().rsplit(':', 1)[1]
        command = ['mysqladmin', '--protocol=tcp', '-h127.0.0.1', '--connect-timeout=1', 'ping'] if kind == 'mysql' else ['pg_isready', '-h127.0.0.1', '-Ucodex_parent', '-dcodex_parent_tests']
        for attempt in range(90):
            ready = subprocess.run(docker + ['exec', container_id, *command], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            if ready.returncode == 0:
                break
            time.sleep(1)
        else:
            raise RuntimeError('临时数据库启动超时：' + kind)
    report['ports'] = ports
    env = os.environ.copy()
    env['CHANNEL_PERIOD_TEST_MYSQL_DSN'] = f'codex_parent:{password}@tcp(127.0.0.1:{ports["mysql"]})/codex_parent_tests?charset=utf8mb4&parseTime=true&loc=Local&timeout=5s'
    env['CHANNEL_PERIOD_TEST_POSTGRES_DSN'] = f'host=127.0.0.1 port={ports["postgres"]} user=codex_parent password={password} dbname=codex_parent_tests sslmode=disable connect_timeout=5'
    with (base / 'database-contract.log').open('w') as output:
        tested = subprocess.run(['go', 'test', './model', '-run', '^TestChannelLimitParentDatabaseMigrationAndConcurrentTopology$', '-count=1', '-v', '-timeout', '120s'], cwd=project, env=env, stdout=output, stderr=subprocess.STDOUT)
    report['passed'] = tested.returncode == 0
    print('三库合同测试' + ('通过' if report['passed'] else '失败，详见 database-contract.log'), flush=True)
finally:
    cleaned = True
    for container_id in created:
        result = subprocess.run(docker + ['inspect', '--format', '{{index .Config.Labels "io.codex.task"}}', container_id], capture_output=True, text=True)
        if result.returncode == 0 and result.stdout.strip() == run_id:
            removed = subprocess.run(docker + ['rm', '--force', '--volumes', container_id], capture_output=True, text=True)
            cleaned = cleaned and removed.returncode == 0
        else:
            cleaned = False
    report['cleaned'] = cleaned
    (base / 'database-contract.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
if not report['passed'] or not report['cleaned']:
    raise SystemExit(1)
