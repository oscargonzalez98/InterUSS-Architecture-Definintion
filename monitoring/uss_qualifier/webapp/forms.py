import flask
import json
from flask_wtf import FlaskForm
from wtforms import (
    StringField,
    SubmitField,
    TextAreaField,
    BooleanField,
    widgets,
    SelectMultipleField,
)
from wtforms.validators import DataRequired, ValidationError


class MultiCheckboxField(SelectMultipleField):
    widget = widgets.ListWidget(prefix_label=False)
    option_widget = widgets.CheckboxInput()


class TestsExecuteForm(FlaskForm):
    flight_records = MultiCheckboxField("Flight Records", choices=[])
    auth_spec = StringField("Auth Spec", validators=[DataRequired()])
    user_config = TextAreaField("User Config", validators=[DataRequired()])
    sample_report = BooleanField("Sample Report")
    submit = SubmitField("Run Test")

    def validate_user_config(form, field):
        try:
            user_config = json.loads(field.data)
        except json.decoder.JSONDecodeError as e:
            raise ValidationError("Invalid User Config object. %s" % str(e))
        expected_keys = {"injection_targets", "observers"}
        if not (user_config.get("rid") or user_config.get("scd")):
            raise ValidationError(
                "One of the `rid` or `scd` fields should be provided in config object."
            )
        if user_config.get("rid"):
            rid_config = user_config["rid"]
            if rid_config and not expected_keys.issubset(set(rid_config)):
                message = f"{rid_config} missing fields in config object {expected_keys - set(rid_config)}"
                raise ValidationError(message)
            if (not form.flight_records.data) or (
                len(form.flight_records.data) < len(rid_config["injection_targets"])
            ):
                raise ValidationError(
                    "Not enough flight states files provided for each injection_targets."
                )

    def validate_flight_records(form, field):
        print("in validate flight: ", field, type(field))
        user_config = json.loads(form.user_config.data)
        if user_config.get("rid"):
            rid_config = user_config["rid"]
            if (not field.data) or (
                len(field.data) < len(rid_config["injection_targets"])
            ):
                raise ValidationError(
                    "Not enough flight states files provided for each injection_targets."
                )


class TestRunsForm(FlaskForm):
    class Meta:
        csrf = False

    flight_records = StringField("Flight Records")
    auth_spec = StringField("Auth Spec", validators=[DataRequired()])
    user_config = TextAreaField("User Config", validators=[DataRequired()])
    sample_report = BooleanField("Sample Report", default=False)
    submit = SubmitField("Run Test")

    def validate_user_config(form, field):
        try:
            user_config = json.loads(field.data)
        except json.decoder.JSONDecodeError as e:
            raise ValidationError("Invalid User Config object. %s" % str(e))
        expected_keys = {"injection_targets", "observers"}
        if not (user_config.get("rid") or user_config.get("scd")):
            raise ValidationError(
                "One of the `rid` or `scd` fields should be provided in config object.%s"
                % user_config
            )
        if user_config.get("rid"):
            rid_config = user_config["rid"]
            if not expected_keys.issubset(set(rid_config)):
                message = f"{rid_config} missing fields in config object {expected_keys - set(rid_config)}"
                raise ValidationError(message)
            if (not form.flight_records.data) or (
                len(form.flight_records.data) < len(rid_config["injection_targets"])
            ):
                raise ValidationError(
                    "Not enough flight states files provided for each injection_targets."
                )

    def validate_flight_records(form, field):
        try:
            user_config = json.loads(form.user_config.data)
        except json.decoder.JSONDecodeError:
            raise ValidationError("Invalid User Config")
        else:
            if user_config.get("rid"):
                rid_config = user_config["rid"]
                if (not field.data) or (
                    len(field.data) < len(rid_config["injection_targets"])
                ):
                    raise ValidationError(
                        "Not enough flight states files provided for each injection_targets."
                    )


def json_abort(status_code, message, details=None):
    data = {"error": {"code": status_code, "message": message}}
    if details:
        data["error"]["details"] = details
    response = flask.jsonify(data)
    response.status_code = status_code
    flask.abort(response)
