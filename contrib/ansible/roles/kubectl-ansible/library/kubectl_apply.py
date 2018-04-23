import os
import json
import shutil
import subprocess
import tempfile
import yaml

from ansible.module_utils.basic import AnsibleModule

class KubectlRunner(object):

    def __init__(self, kubeconfig, context=None):
        self.kubeconfig = kubeconfig
        self.context = context

    # following approach from lib_openshift
    def run(self, cmds, input_data):
        ''' Actually executes the command. This makes mocking easier. '''
        curr_env = os.environ.copy()
        curr_env.update({'KUBECONFIG': self.kubeconfig})
        proc = subprocess.Popen(cmds,
                                stdin=subprocess.PIPE,
                                stdout=subprocess.PIPE,
                                stderr=subprocess.PIPE,
                                env=curr_env)

        encoded_input = input_data.encode() if input_data else None
        # TODO: encode here was required on Python 3, watchout for 2:
        stdout, stderr = proc.communicate(encoded_input)
        return proc.returncode, stdout.decode('utf-8'), stderr.decode('utf-8')


class KubectlApplier(object):
    def __init__(self, kubeconfig=None, context=None, namespace=None, definition=None, src=None, debug=None):
        self.kubeconfig = kubeconfig
        self.context = context
        self.namespace = namespace
        self.definition = definition

        # Loads as a dict, convert to a json string for piping to kubectl:
        #self.definition = json.dumps(definition)
        self.src = src

        self.cmds = ["kubectl", "apply"]

        if self.namespace:
            self.cmds.extend(["-n", self.namespace])
        self.cmd_runner = KubectlRunner(self.kubeconfig, self.context)

        self.changed = False
        self.failed = False
        self.debug_lines = []
        self.stdout_lines = []
        self.stderr_lines = []

    def run(self):
        self.debug_lines.append("using kubeconfig: %s" % self.kubeconfig)
        exit_code, stdout, stderr = (None, None, None)

        # Switch context if necessary:
        if self.context:
            exit_code, stdout, stderr = self.cmd_runner.run(
                    ["kubectl", "config", "use-context", self.context], None)
            self._process_cmd_result(exit_code, stdout, stderr)
            if self.failed:
                return

        if self.definition:
            self.cmds.extend(["-f", "-"])
            # We end up with a string here containing json, but using single quotes instead of double,
            # which does not parse as valid json. Replace them instead so kubectl is happy:
            # TODO: is this right?
            self.definition = self.definition.replace('\'', '"')
            self.debug_lines.append('definition: %s' % self.definition)
            exit_code, stdout, stderr = self.cmd_runner.run(self.cmds, self.definition)
            self._process_cmd_result(exit_code, stdout, stderr)
            if self.failed:
                return
        elif self.src:
            self.debug_lines.append('src: %s' % self.src)
            self.cmds.extend(["-f", self.src])
            # path = os.path.normpath(src)
            # if not os.path.exists(path):
                # self.fail_json(msg="Error accessing {0}. Does the file exist?".format(path))
            exit_code, stdout, stderr = self.cmd_runner.run(self.cmds, None)
            self._process_cmd_result(exit_code, stdout, stderr)
            if self.failed:
                return

    def _process_cmd_result(self, exit_code, stdout, stderr):
        if stdout:
            self.stdout_lines.extend(stdout.split('\n'))
        if stderr:
            self.stderr_lines.extend(stderr.split('\n'))
        self.changed = self.changed or self._check_stdout_for_changes(self.stdout_lines)
        self.failed = self.failed or exit_code > 0

    def _check_stdout_for_changes(self, stdout_lines):
        """
        kubectl apply will print lines such as:

          namespace "testnamespace" created
          namespace "testnamespace" configured
          namespace "testnamespace" changed

        To hack around the inability to know if something changed we'll parse stdout lines
        looking for anytihng ending with either "created" or "configured". This should work for
        commands that create/update multiple objects.
        """
        for line in stdout_lines:
            if line.endswith(" created") or line.endswith(" configured"):
                return True
        return False


class KubectlApplyWrapperModule(AnsibleModule):
    def __init__(self, *args, **kwargs):
        AnsibleModule.__init__(self, argument_spec=dict(
            kubeconfig=dict(required=False, type='dict'),
            context=dict(required=False, type='str'),
            namespace=dict(required=False, type='str'),
            debug=dict(required=False, type='bool', default='false'),
            definition=dict(required=False, type='str'),
            src=dict(required=False, type='str'),
        ))
        # TODO: check for conflicting modules

    def execute_module(self):
        # Temporary copy of kubeconfig specified, we will always clean this up after execution:
        temp_kubeconfig_path = None

        kubeconfig = self.params['kubeconfig']
        if not kubeconfig:
            kubeconfig = {}

        if 'file' in kubeconfig and 'inline' in kubeconfig:
            self.fail_json(msg="cannot specify both 'file' and 'inline' for kubeconfig")

        # If no kubeconfig was provided, use the default location:
        if 'file' not in kubeconfig:
            kubeconfig['file'] = os.path.expanduser("~/.kube/config")

        if 'inline' in kubeconfig:
            fd, temp_kubeconfig_path = tempfile.mkstemp(prefix="ansible-tmp-kubeconfig-")
            with open(temp_kubeconfig_path, 'w') as f:
                f.write(kubeconfig['inline'])
            os.close(fd)

        else:
            # copy the kubeconfig so we can safely switch contexts:
            if not os.path.exists(kubeconfig['file']):
                self.fail_json(msg="kubeconfig file does not exist: %s" % kubeconfig['file'])
            fd, temp_kubeconfig_path = tempfile.mkstemp(prefix="ansible-tmp-kubeconfig-")
            shutil.copy2(kubeconfig['file'], temp_kubeconfig_path)

        applier = KubectlApplier(
            kubeconfig=temp_kubeconfig_path,
            context=self.params['context'],
            namespace=self.params['namespace'],
            definition=self.params['definition'],
            src=self.params['src'],
            debug=self.boolean(self.params['debug']))

        applier.run()

        # Cleanup:
        os.remove(temp_kubeconfig_path)

        if applier.failed:
            self.fail_json(
                msg="error executing kubectl apply",
                debug=applier.debug_lines,
                stderr_lines=applier.stderr_lines,
                stdout_lines=applier.stdout_lines)
        else:
            self.exit_json(
                changed=applier.changed,
                debug=applier.debug_lines,
                stderr_lines=applier.stderr_lines,
                stdout_lines=applier.stdout_lines)


def main():
    KubectlApplyWrapperModule().execute_module()


if __name__ == '__main__':
    main()
