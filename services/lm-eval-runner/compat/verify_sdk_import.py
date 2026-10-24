"""Verify real public LM SDK imports only in the fixed offline SDK layout."""
from pathlib import Path
from importlib.metadata import distribution,distributions,version
from importlib.util import find_spec
import argparse
import hashlib
import inspect
import json
import os
import re
import sys
import sysconfig


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--site-packages',type=Path,required=True)
    parser.add_argument('--receipt',type=Path,required=True)
    args=parser.parse_args()
    if (os.environ.get('HAI_DISPOSABLE_PUBLIC_SDK')!='1' or sys.platform!='linux'
            or sys.version_info[:3]!=(3,15,1) or sysconfig.get_config_var('Py_GIL_DISABLED')
            or args.site_packages.resolve()!=Path('/installed')
            or args.receipt!=Path('/qualification/sdk-import.json')):
        raise RuntimeError('Real LM SDK imports require the fixed hosted offline layout')
    sys.path.insert(0,'/installed')
    normalize=lambda name:re.sub(r'[-_.]+','-',name.lower())
    expected=json.loads(Path('/inputs/expected-distributions.json').read_text(encoding='utf-8'))
    actual={normalize(d.metadata['Name']):d.version for d in distributions(path=['/installed'])}
    if actual!=expected or len(actual)!=70:raise ValueError('Installed graph differs from resolved current candidate')
    required={'lm-eval':'0.4.13+hai.optionalcache.1','datasets':'5.1.0+hai.fsspec.1',
              'mbstrdecoder':'1.1.5+hai.chardet7.1','pytablewriter':'1.2.1+hai.typepy2.1',
              'typepy':'2.0.0+hai.dateutil3.1','chardet':'7.7.0','fsspec':'2026.9.0','python-dateutil':'3.0.0'}
    if any(actual.get(k)!=v for k,v in required.items()) or find_spec('sqlitedict') is not None:
        raise ValueError('Required current graph or prohibited cache boundary differs')
    specs=[{'name':'lm-eval',**json.loads(Path('/inputs/port-spec.json').read_text(encoding='utf-8'))},
           *json.loads(Path('/inputs/python-parent-port-specs.json').read_text(encoding='utf-8'))]
    source_counts={}
    for spec in specs:
        package=distribution(spec['name'])
        sources={str(p):hashlib.sha256(p.locate().read_bytes()).hexdigest()
                 for p in package.files if str(p).endswith('.py')}
        manifest=hashlib.sha256(json.dumps(sources,sort_keys=True,separators=(',',':')).encode()).hexdigest()
        if len(sources)!=spec['unchanged_python_file_count'] or manifest!=spec['unchanged_python_manifest_sha256']:
            raise ValueError('Installed engine/parent source manifest differs: '+spec['name'])
        source_counts[spec['name']]=len(sources)
    entrypoints={p.name:p.value for p in distribution('lm-eval').entry_points if p.group=='console_scripts'}
    if entrypoints!={'lm-eval':'lm_eval.__main__:cli_evaluate','lm_eval':'lm_eval.__main__:cli_evaluate'}:
        raise ValueError('LM CLI entrypoints changed')
    import lm_eval
    from lm_eval.api.model import LM
    from lm_eval.models.openai_completions import LocalChatCompletion
    from lm_eval.utils import make_table
    from lm_eval.__main__ import cli_evaluate
    import datasets
    from fsspec.core import url_to_fs
    from mbstrdecoder import MultiByteStrDecoder
    from chardet.universaldetector import UniversalDetector
    import chardet
    from pytablewriter import MarkdownTableWriter
    from typepy import DateTime,StrictLevel,Typecode
    import dateutil.parser
    for symbol in [LM,LocalChatCompletion,make_table,cli_evaluate,url_to_fs,MultiByteStrDecoder,
                   UniversalDetector,chardet.detect,MarkdownTableWriter,DateTime,dateutil.parser.parse]:
        if not callable(symbol):raise ValueError('Required real engine/parent interface is unavailable')
    for symbol in [make_table,url_to_fs,chardet.detect,dateutil.parser.parse]:inspect.signature(symbol)
    if lm_eval.__version__!=required['lm-eval']:raise ValueError('LM module version differs from explicit distribution')
    result={'python':'.'.join(map(str,sys.version_info[:3])),'standard_GIL_build':True,
            'installed_versions':actual,'unchanged_installed_Python_files':source_counts,
            'CLI_entrypoints_preserved':True,'real_engine_and_parent_interfaces_imported':True,
            'model_or_evaluator_instantiated':False,'model_requests_or_private_data_used':False,
            'SqliteDict_installed':False,'application_cases_run':False,
            'functional_cases_or_full_image_provider_acceptance':False}
    args.receipt.write_text(json.dumps(result,indent=2)+'\n',encoding='utf-8')
    print(json.dumps(result))


if __name__=='__main__':main()
