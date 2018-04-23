import os
import json
import shutil
import subprocess
import tempfile
import yaml

from ansible.module_utils.basic import AnsibleModule


# TODO: duplicated with kubectl_apply.py, figure out how to share this code.
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
    def __init__(self, kubeconfig=None, context=None, namespace=None, template_definition=None, template_file=None, parameters=None, debug=None):
        self.kubeconfig = kubeconfig
        self.context = context
        self.namespace = namespace
        self.parameters = parameters
        self.template_definition = template_definition

        # Loads as a dict, convert to a json string for piping to kubectl:
        self.template_file = template_file

        self.cmds = ["oc", "process"]

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
        self.debug_lines.append("parameters: %s" % self.parameters)
        exit_code, stdout, stderr = (None, None, None)

        # Switch context if necessary:
        if self.context:
            exit_code, stdout, stderr = self.cmd_runner.run(
                    ["kubectl", "config", "use-context", self.context], None)
            self._process_cmd_result(exit_code, stdout, stderr)
            if self.failed:
                return

        if self.parameters:
            for k in self.parameters:
                self.cmds.extend(["-p", "%s=%s" % (k, self.parameters[k])])

        if self.template_definition:
            self.cmds.extend(["-f", "-"])
            # We end up with a string here containing json, but using single quotes instead of double,
            # which does not parse as valid json. Replace them instead so kubectl is happy:
            # TODO: is this right?
            # TODO: nope definition not right.
            self.template_definition = self.template_definition.replace('\'', '"')
            self.template_definition = self.template_definition.replace('True', 'true')
            self.template_definition = self.template_definition.replace('False', 'false')

            self.debug_lines.append('template_definition: %s' % self.template_definition)
            exit_code, stdout, stderr = self.cmd_runner.run(self.cmds, self.template_definition)
            self._process_cmd_result(exit_code, stdout, stderr)
            if self.failed:
                return
        elif self.template_file:
            self.debug_lines.append('template_file: %s' % self.template_file)
            self.cmds.extend(["-f", self.template_file])
            # path = os.path.normpath(template_file)
            # if not os.path.exists(path):
                # self.fail_json(msg="Error accessing {0}. Does the file exist?".format(path))
            exit_code, stdout, stderr = self.cmd_runner.run(self.cmds, None)
            self._process_cmd_result(exit_code, stdout, stderr)
            if self.failed:
                return

    def _process_cmd_result(self, exit_code, stdout, stderr):
        self.stdout = stdout
        if stderr:
            self.stderr_lines.extend(stderr.split('\n'))
        # Template processing does not change anything itself.
        self.changed = False
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


class OcProcessModule(AnsibleModule):
    def __init__(self, *args, **kwargs):
        # TODO: add support for template name, pre-existing on server
        # TODO: check for conflicting params
        # TODO: support parameter files
        AnsibleModule.__init__(self, argument_spec=dict(
            kubeconfig=dict(required=False, type='dict'),
            context=dict(required=False, type='str'),
            namespace=dict(required=False, type='str'),
            debug=dict(required=False, type='bool', default='false'),
            template_definition=dict(required=False, type='str'),
            template_file=dict(required=False, type='str'),
            parameters=dict(required=False, type='dict'),
        ))

    def _configure_kubeconfig(self):
        """ Check kubeconfig parameters and copy to temporary file. Returns path to that file. """
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

        # TODO: probably could switch contexts here

        return temp_kubeconfig_path


    def execute_module(self):

        # List of temp files we will cleanup:
        temp_files = []

        temp_kubeconfig_path = self._configure_kubeconfig()
        temp_files.append(temp_kubeconfig_path)

        applier = KubectlApplier(
            kubeconfig=temp_kubeconfig_path,
            context=self.params['context'],
            namespace=self.params['namespace'],
            template_file=self.params['template_file'],
            template_definition=self.params['template_definition'],
            parameters=self.params['parameters'],
            debug=self.boolean(self.params['debug']))

        applier.run()

        # Cleanup:
        for cleanup_file in temp_files:
            os.remove(cleanup_file)

        # Attempt to parse the json output:
        result = ""
        try:
            result = json.loads(applier.stdout)
        except Exception, e:
            pass

        if applier.failed:
            self.fail_json(
                msg="error executing oc process",
                debug=applier.debug_lines,
                stderr_lines=applier.stderr_lines,
                result=result)
        else:
            self.exit_json(
                changed=applier.changed,
                debug=applier.debug_lines,
                stderr_lines=applier.stderr_lines,
                result=result)


def main():
    OcProcessModule().execute_module()


if __name__ == '__main__':
    main()
