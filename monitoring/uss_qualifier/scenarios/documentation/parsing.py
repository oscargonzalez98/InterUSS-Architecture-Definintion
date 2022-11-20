import inspect
import os
from typing import List, Dict, Type

import marko
import marko.element
import marko.inline

from monitoring import uss_qualifier as uss_qualifier_module
from monitoring.monitorlib.inspection import fullname, get_module_object_by_name
from monitoring.uss_qualifier.scenarios.documentation.definitions import (
    TestStepDocumentation,
    TestCheckDocumentation,
    TestCaseDocumentation,
    TestScenarioDocumentation,
)

RESOURCES_HEADING = "resources"
CLEANUP_HEADING = "cleanup"
TEST_SCENARIO_SUFFIX = " test scenario"
TEST_CASE_SUFFIX = " test case"
TEST_STEP_SUFFIX = " test step"
TEST_CHECK_SUFFIX = " check"


_test_step_cache: Dict[str, TestStepDocumentation] = {}


def _text_of(value) -> str:
    if isinstance(value, str):
        return value
    elif isinstance(value, marko.block.BlockElement):
        result = ""
        for child in value.children:
            result += _text_of(child)
        return result
    elif isinstance(value, marko.inline.InlineElement):
        if isinstance(value.children, str):
            return value.children
        result = ""
        for child in value.children:
            result += _text_of(child)
        return result
    else:
        raise NotImplementedError(
            "Cannot yet extract raw text from {}".format(value.__class__.__name__)
        )


def _length_of_section(values, start_of_section: int) -> int:
    level = values[start_of_section].level
    c = start_of_section + 1
    while c < len(values):
        if isinstance(values[c], marko.block.Heading) and values[c].level == level:
            break
        c += 1
    return c - start_of_section - 1


def _parse_test_check(values) -> TestCheckDocumentation:
    name = _text_of(values[0])[0 : -len(TEST_CHECK_SUFFIX)]

    reqs: List[str] = []
    c = 1
    while c < len(values):
        if isinstance(values[c], marko.block.Paragraph):
            for child in values[c].children:
                if isinstance(child, marko.inline.StrongEmphasis):
                    reqs.append(_text_of(child))
        c += 1

    return TestCheckDocumentation(name=name, applicable_requirements=reqs)


def _get_linked_test_step(
    doc_filename: str, origin_filename: str
) -> TestStepDocumentation:
    absolute_path = os.path.abspath(
        os.path.join(os.path.dirname(origin_filename), doc_filename)
    )
    if absolute_path not in _test_step_cache:
        if not os.path.exists(absolute_path):
            raise ValueError(
                f'Test step document "{doc_filename}" linked from "{origin_filename}" does not exist at "{absolute_path}"'
            )
        with open(absolute_path, "r") as f:
            doc = marko.parse(f.read())

        if (
            not isinstance(doc.children[0], marko.block.Heading)
            or doc.children[0].level != 1
            or not _text_of(doc.children[0]).lower().endswith(TEST_STEP_SUFFIX)
        ):
            raise ValueError(
                f'The first line of "{absolute_path}" must be a level-1 heading with the name of the test step + "{TEST_STEP_SUFFIX}" (e.g., "# Successful flight injection{TEST_STEP_SUFFIX}")'
            )

        values = doc.children
        dc = _length_of_section(values, 0)
        _test_step_cache[absolute_path] = _parse_test_step(
            values[0 : dc + 1], doc_filename
        )
    return _test_step_cache[absolute_path]


def _parse_test_step(values, doc_filename: str) -> TestStepDocumentation:
    name = _text_of(values[0])
    if name.lower().endswith(TEST_STEP_SUFFIX):
        name = name[0 : -len(TEST_STEP_SUFFIX)]

    if values[0].children and isinstance(
        values[0].children[0], marko.block.inline.Link
    ):
        # We should include the content of the linked test step document rather
        # than extracting content from this section.
        step = _get_linked_test_step(values[0].children[0].dest, doc_filename)
        step = TestStepDocumentation(step)
        step.name = name
        return step

    checks: List[TestCheckDocumentation] = []
    c = 1
    while c < len(values):
        if isinstance(values[c], marko.block.Heading) and _text_of(
            values[c]
        ).lower().endswith(TEST_CHECK_SUFFIX):
            # Start of a test step section
            dc = _length_of_section(values, c)
            check = _parse_test_check(values[c : c + dc + 1])
            checks.append(check)
            c += dc
        else:
            c += 1

    return TestStepDocumentation(name=name, checks=checks)


def _parse_test_case(values, doc_filename: str) -> TestCaseDocumentation:
    name = _text_of(values[0])[0 : -len(TEST_CASE_SUFFIX)]

    steps: List[TestStepDocumentation] = []
    c = 1
    while c < len(values):
        if isinstance(values[c], marko.block.Heading) and _text_of(
            values[c]
        ).lower().endswith(TEST_STEP_SUFFIX):
            # Start of a test step section
            dc = _length_of_section(values, c)
            step = _parse_test_step(values[c : c + dc + 1], doc_filename)
            steps.append(step)
            c += dc
        else:
            c += 1

    return TestCaseDocumentation(name=name, steps=steps)


def _parse_resources(values) -> List[str]:
    resource_level = values[0].level + 1
    resources: List[str] = []
    c = 1
    while c < len(values):
        if (
            isinstance(values[c], marko.block.Heading)
            and values[c].level == resource_level
        ):
            # This is a resource
            resources.append(_text_of(values[c]))
        c += 1
    return resources


def get_documentation_filename(scenario: Type) -> str:
    return os.path.splitext(inspect.getfile(scenario))[0] + ".md"


def _parse_documentation(scenario: Type) -> TestScenarioDocumentation:
    # Load the .md file matching the Python file where this scenario type is defined
    doc_filename = get_documentation_filename(scenario)
    if not os.path.exists(doc_filename):
        raise ValueError(
            "Test scenario `{}` does not have the required documentation file `{}`".format(
                fullname(scenario), doc_filename
            )
        )
    with open(doc_filename, "r") as f:
        doc = marko.parse(f.read())

    # Extract the scenario name from the first top-level header
    if (
        not isinstance(doc.children[0], marko.block.Heading)
        or doc.children[0].level != 1
        or not _text_of(doc.children[0]).lower().endswith(TEST_SCENARIO_SUFFIX)
    ):
        raise ValueError(
            'The first line of {} must be a level-1 heading with the name of the scenario + "{}" (e.g., "# ASTM NetRID nominal behavior test scenario")'.format(
                doc_filename, TEST_SCENARIO_SUFFIX
            )
        )
    scenario_name = _text_of(doc.children[0])[0 : -len(TEST_SCENARIO_SUFFIX)]

    # Step through the document to extract important structured components
    test_cases: List[TestCaseDocumentation] = []
    resources = None
    cleanup = None
    c = 1
    while c < len(doc.children):
        if not isinstance(doc.children[c], marko.block.Heading):
            c += 1
            continue

        if _text_of(doc.children[c]).lower().strip() == RESOURCES_HEADING:
            # Start of the Resources section
            if resources is not None:
                raise ValueError(
                    f'Only one major section may be titled "{RESOURCES_HEADING}"'
                )
            dc = _length_of_section(doc.children, c)
            resources = _parse_resources(doc.children[c : c + dc + 1])
            c += dc
        elif _text_of(doc.children[c]).lower().strip() == CLEANUP_HEADING:
            # Start of the Cleanup section
            if cleanup is not None:
                raise ValueError(
                    'Only one major section may be titled "{CLEANUP_HEADING}"'
                )
            dc = _length_of_section(doc.children, c)
            cleanup = _parse_test_step(doc.children[c : c + dc + 1], doc_filename)
            c += dc
        elif _text_of(doc.children[c]).lower().endswith(TEST_CASE_SUFFIX):
            # Start of a test case section
            dc = _length_of_section(doc.children, c)
            test_case = _parse_test_case(doc.children[c : c + dc + 1], doc_filename)
            test_cases.append(test_case)
            c += dc
        else:
            c += 1

    kwargs = {
        # TODO: Populate the documentation URLs
        "name": scenario_name,
        "cases": test_cases,
        "resources": resources,
        "url": "",
    }
    if cleanup is not None:
        kwargs["cleanup"] = cleanup
    return TestScenarioDocumentation(**kwargs)


def get_documentation(scenario: Type) -> TestScenarioDocumentation:
    DOC_CACHE_ATTRIBUTE = "_md_documentation"
    if not hasattr(scenario, DOC_CACHE_ATTRIBUTE):
        setattr(scenario, DOC_CACHE_ATTRIBUTE, _parse_documentation(scenario))
    return getattr(scenario, DOC_CACHE_ATTRIBUTE)


def get_documentation_by_name(scenario_type_name: str) -> TestScenarioDocumentation:
    scenario_type = get_module_object_by_name(uss_qualifier_module, scenario_type_name)
    return get_documentation(scenario_type)
