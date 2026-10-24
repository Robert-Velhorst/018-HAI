"""Run unchanged upstream cases only in the fixed network-isolated SDK layout."""
from pathlib import Path
import argparse
import hashlib
import json
import os
import sys
import sysconfig
import xml.etree.ElementTree as ET


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--suite',choices=['datasets','mbstrdecoder','pytablewriter','typepy'],required=True)
    args=parser.parse_args()
    if (os.environ.get('HAI_DISPOSABLE_UPSTREAM_CASES')!='1' or sys.platform!='linux'
            or sys.version_info[:3]!=(3,15,1) or sysconfig.get_config_var('Py_GIL_DISABLED')):
        raise RuntimeError('Existing cases require the fixed disposable standard-GIL CPython SDK')
    source=Path('/upstream')
    specs=json.loads(Path('/inputs/upstream-case-specs.json').read_text(encoding='utf-8'))
    spec=next(s for s in specs if s['name']==args.suite)
    for item in spec['files']:
        path=source/item['path']
        if path.is_symlink() or not path.resolve(strict=True).is_relative_to(source):
            raise ValueError('Existing case source escapes the fixed source root')
        raw=path.read_bytes()
        if len(raw)!=item['bytes'] or hashlib.sha256(raw).hexdigest()!=item['sha256']:
            raise ValueError('Existing upstream case/helper/license was modified')
    for selector in spec['selectors']:
        if not (selector=='test' or selector.startswith(('test/','tests/'))) or '..' in selector.split('::')[0].split('/'):
            raise ValueError('Unexpected existing case selector')
    sys.path[:0]=['/upstream','/case-tools','/installed']
    from importlib.metadata import distributions
    import re
    normalize=lambda name:re.sub(r'[-_.]+','-',name.lower())
    expected=json.loads(Path('/inputs/expected-distributions.json').read_text(encoding='utf-8'))
    runtime={normalize(d.metadata['Name']):d for d in distributions(path=['/installed'])}
    tool_distributions={normalize(d.metadata['Name']):d for d in distributions(path=['/case-tools'])}
    tools={name:d.version for name,d in tool_distributions.items()}
    overlap_source_counts={}
    for name in set(expected)&set(tools):
        if expected[name]!=tools[name]:raise ValueError('Test tool shadows the qualified runtime version: '+name)
        source_manifest=lambda d:{str(p):hashlib.sha256(p.locate().read_bytes()).hexdigest()
                                  for p in d.files if str(p).endswith(('.py','.so'))}
        before,after=source_manifest(runtime[name]),source_manifest(tool_distributions[name])
        if before!=after:raise ValueError('Test tool shadows qualified Python/native source bytes: '+name)
        overlap_source_counts[name]=len(before)
    # Datasets' published test helpers use distutils.util. The declared current
    # Setuptools test dependency supplies its normal implementation; no module
    # stub, monkeypatch or warning suppression is introduced here.
    import setuptools
    import pytest
    os.chdir('/tmp')
    junit=Path('/qualification')/(args.suite+'-upstream-cases.xml')
    selectors=[str(source/s.split('::')[0])+('::'+'::'.join(s.split('::')[1:]) if '::' in s else '')
               for s in spec['selectors']]
    code=int(pytest.main([*selectors,'-q','-o','cache_dir=/tmp/pytest-cache',
                          '--junitxml='+str(junit)]))
    cases=ET.parse(junit).getroot().findall('.//testcase') if junit.exists() else []
    skipped=sum(c.find('skipped') is not None for c in cases)
    failures=sum(c.find('failure') is not None for c in cases)
    errors=sum(c.find('error') is not None for c in cases)
    passed=sum(all(c.find(k) is None for k in ['failure','error','skipped']) for c in cases)
    result={'suite':args.suite,'pytest_returncode':code,'case_items_observed':len(cases),
            'passed':passed,'skipped':skipped,'failures':failures,'errors':errors,
            'source_files_sha256_verified':len(spec['files']),'identical_runtime_tool_source_file_counts':overlap_source_counts,
            'all_cases_and_helpers_unchanged':True,'network_mode':'none',
            'operator_data_or_credentials_mounted':False,
            'new_application_cases_authored':False,'real_model_or_agent_invoked':False,
            'full_data_or_provider_acceptance':False,
            'qualified':code==0 and passed>0 and skipped==0 and failures==0 and errors==0}
    (Path('/qualification')/(args.suite+'-upstream-cases.json')).write_text(json.dumps(result,indent=2)+'\n',encoding='utf-8')
    print(json.dumps(result))
    if not result['qualified']:raise SystemExit(code or 1)


if __name__=='__main__':main()
