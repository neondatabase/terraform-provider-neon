package provider

import (
	"fmt"

	schemaDataSource "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	schemaResource "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

type t interface {
	bool | string | int | int32 | int64 | float64 | float32 | neon.PgVersion | neon.ComputeUnit | neon.Provisioner | neon.SuspendTimeoutSeconds
}

func pointer[V t](v V) *V {
	if fmt.Sprintf("%v", v) == "" {
		return nil
	}
	return &v
}

func stringChanged(prior, planned types.String) bool {
	if prior.IsUnknown() || planned.IsUnknown() || prior.IsNull() || planned.IsNull() {
		return false
	}
	return prior.ValueString() != planned.ValueString()
}

func newReadOnlyResourceAttrFromResourceAttr(attr schemaResource.Attribute) schemaResource.Attribute {
	switch attr := attr.(type) {
	case schemaResource.StringAttribute:
		return schemaResource.StringAttribute{
			Computed:            true,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.BoolAttribute:
		return schemaResource.BoolAttribute{
			Computed:            true,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.Float32Attribute:
		return schemaResource.Float32Attribute{
			Computed:            true,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.Float64Attribute:
		return schemaResource.Float64Attribute{
			Computed:            true,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.Int32Attribute:
		return schemaResource.Int32Attribute{
			Computed:            true,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.Int64Attribute:
		return schemaResource.Int64Attribute{
			Computed:            true,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.NumberAttribute:
		return schemaResource.NumberAttribute{
			Computed:            true,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.DynamicAttribute:
		return schemaResource.DynamicAttribute{
			Computed:            true,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.ListAttribute:
		return schemaResource.ListAttribute{
			Computed:            true,
			ElementType:         attr.ElementType,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.SetAttribute:
		return schemaResource.SetAttribute{
			Computed:            true,
			ElementType:         attr.ElementType,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.ObjectAttribute:
		return schemaResource.ObjectAttribute{
			Computed:            true,
			AttributeTypes:      attr.AttributeTypes,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.MapAttribute:
		return schemaResource.MapAttribute{
			Computed:            true,
			ElementType:         attr.ElementType,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.SingleNestedAttribute:
		o := schemaResource.SingleNestedAttribute{
			Computed:            true,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
			CustomType:          attr.CustomType,
			Attributes:          make(map[string]schemaResource.Attribute, len(attr.Attributes)),
		}
		for k, v := range attr.Attributes {
			o.Attributes[k] = newReadOnlyResourceAttrFromResourceAttr(v)
		}
		return o

	case schemaResource.ListNestedAttribute:
		o := schemaResource.ListNestedAttribute{
			Computed:            true,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
			CustomType:          attr.CustomType,
			NestedObject: schemaResource.NestedAttributeObject{
				CustomType: attr.NestedObject.CustomType,
				Attributes: make(map[string]schemaResource.Attribute, len(attr.NestedObject.Attributes)),
			},
		}
		for k, v := range attr.NestedObject.Attributes {
			o.NestedObject.Attributes[k] = newReadOnlyResourceAttrFromResourceAttr(v)
		}
		return o

	case schemaResource.SetNestedAttribute:
		o := schemaResource.SetNestedAttribute{
			Computed:            true,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
			CustomType:          attr.CustomType,
			NestedObject: schemaResource.NestedAttributeObject{
				CustomType: attr.NestedObject.CustomType,
				Attributes: make(map[string]schemaResource.Attribute, len(attr.NestedObject.Attributes)),
			},
		}
		for k, v := range attr.NestedObject.Attributes {
			o.NestedObject.Attributes[k] = newReadOnlyResourceAttrFromResourceAttr(v)
		}
		return o

	case schemaResource.MapNestedAttribute:
		o := schemaResource.MapNestedAttribute{
			Computed:            true,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
			CustomType:          attr.CustomType,
			NestedObject: schemaResource.NestedAttributeObject{
				CustomType: attr.NestedObject.CustomType,
				Attributes: make(map[string]schemaResource.Attribute, len(attr.NestedObject.Attributes)),
			},
		}
		for k, v := range attr.NestedObject.Attributes {
			o.NestedObject.Attributes[k] = newReadOnlyResourceAttrFromResourceAttr(v)
		}
		return o

	default:
		return nil
	}
}

func newReadOnlyDataSourceAttrFromResourceAttr(attr schemaResource.Attribute) schemaDataSource.Attribute {
	switch attr := attr.(type) {
	case schemaResource.StringAttribute:
		return schemaDataSource.StringAttribute{
			Computed:            true,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.BoolAttribute:
		return schemaDataSource.BoolAttribute{
			Computed:            true,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.Float32Attribute:
		return schemaDataSource.Float32Attribute{
			Computed:            true,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.Float64Attribute:
		return schemaDataSource.Float64Attribute{
			Computed:            true,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.Int32Attribute:
		return schemaDataSource.Int32Attribute{
			Computed:            true,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.Int64Attribute:
		return schemaDataSource.Int64Attribute{
			Computed:            true,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.NumberAttribute:
		return schemaDataSource.NumberAttribute{
			Computed:            true,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.DynamicAttribute:
		return schemaDataSource.DynamicAttribute{
			Computed:            true,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.ListAttribute:
		return schemaDataSource.ListAttribute{
			Computed:            true,
			ElementType:         attr.ElementType,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.SetAttribute:
		return schemaDataSource.SetAttribute{
			Computed:            true,
			ElementType:         attr.ElementType,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.ObjectAttribute:
		return schemaDataSource.ObjectAttribute{
			Computed:            true,
			AttributeTypes:      attr.AttributeTypes,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.MapAttribute:
		return schemaDataSource.MapAttribute{
			Computed:            true,
			ElementType:         attr.ElementType,
			CustomType:          attr.CustomType,
			Sensitive:           attr.Sensitive,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
		}

	case schemaResource.SingleNestedAttribute:
		o := schemaDataSource.SingleNestedAttribute{
			Computed:            true,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
			CustomType:          attr.CustomType,
			Attributes:          make(map[string]schemaDataSource.Attribute, len(attr.Attributes)),
		}
		for k, v := range attr.Attributes {
			o.Attributes[k] = newReadOnlyDataSourceAttrFromResourceAttr(v)
		}
		return o

	case schemaResource.ListNestedAttribute:
		o := schemaDataSource.ListNestedAttribute{
			Computed:            true,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
			CustomType:          attr.CustomType,
			NestedObject: schemaDataSource.NestedAttributeObject{
				CustomType: attr.NestedObject.CustomType,
				Attributes: make(map[string]schemaDataSource.Attribute, len(attr.NestedObject.Attributes)),
			},
		}
		for k, v := range attr.NestedObject.Attributes {
			o.NestedObject.Attributes[k] = newReadOnlyDataSourceAttrFromResourceAttr(v)
		}
		return o

	case schemaResource.SetNestedAttribute:
		o := schemaDataSource.SetNestedAttribute{
			Computed:            true,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
			CustomType:          attr.CustomType,
			NestedObject: schemaDataSource.NestedAttributeObject{
				CustomType: attr.NestedObject.CustomType,
				Attributes: make(map[string]schemaDataSource.Attribute, len(attr.NestedObject.Attributes)),
			},
		}
		for k, v := range attr.NestedObject.Attributes {
			o.NestedObject.Attributes[k] = newReadOnlyDataSourceAttrFromResourceAttr(v)
		}
		return o

	case schemaResource.MapNestedAttribute:
		o := schemaDataSource.MapNestedAttribute{
			Computed:            true,
			Description:         attr.Description,
			MarkdownDescription: attr.MarkdownDescription,
			DeprecationMessage:  attr.DeprecationMessage,
			CustomType:          attr.CustomType,
			NestedObject: schemaDataSource.NestedAttributeObject{
				CustomType: attr.NestedObject.CustomType,
				Attributes: make(map[string]schemaDataSource.Attribute, len(attr.NestedObject.Attributes)),
			},
		}
		for k, v := range attr.NestedObject.Attributes {
			o.NestedObject.Attributes[k] = newReadOnlyDataSourceAttrFromResourceAttr(v)
		}
		return o

	default:
		return nil
	}
}

func newReadOnlyDataSourceBlockFromResourceBlock(block schemaResource.Block) schemaDataSource.Block {
	switch block := block.(type) {
	case schemaResource.SingleNestedBlock:
		attributes, blocks := newReadOnlyDataSourceNestedBlockContents(block.Attributes, block.Blocks)
		return schemaDataSource.SingleNestedBlock{
			Description:         block.Description,
			MarkdownDescription: block.MarkdownDescription,
			DeprecationMessage:  block.DeprecationMessage,
			CustomType:          block.CustomType,
			Attributes:          attributes,
			Blocks:              blocks,
		}

	case schemaResource.ListNestedBlock:
		return schemaDataSource.ListNestedBlock{
			Description:         block.Description,
			MarkdownDescription: block.MarkdownDescription,
			DeprecationMessage:  block.DeprecationMessage,
			CustomType:          block.CustomType,
			NestedObject:        newReadOnlyDataSourceNestedBlockObject(block.NestedObject),
		}

	case schemaResource.SetNestedBlock:
		return schemaDataSource.SetNestedBlock{
			Description:         block.Description,
			MarkdownDescription: block.MarkdownDescription,
			DeprecationMessage:  block.DeprecationMessage,
			CustomType:          block.CustomType,
			NestedObject:        newReadOnlyDataSourceNestedBlockObject(block.NestedObject),
		}

	default:
		panic("unsupported resource block type")
	}
}

func newReadOnlyDataSourceNestedBlockObject(object schemaResource.NestedBlockObject) schemaDataSource.NestedBlockObject {
	attributes, blocks := newReadOnlyDataSourceNestedBlockContents(object.Attributes, object.Blocks)

	return schemaDataSource.NestedBlockObject{
		Attributes: attributes,
		Blocks:     blocks,
	}
}

func newReadOnlyDataSourceNestedBlockContents(
	resourceAttributes map[string]schemaResource.Attribute,
	resourceBlocks map[string]schemaResource.Block,
) (map[string]schemaDataSource.Attribute, map[string]schemaDataSource.Block) {
	attributes := make(map[string]schemaDataSource.Attribute, len(resourceAttributes))
	for name, attr := range resourceAttributes {
		attributes[name] = newReadOnlyDataSourceAttrFromResourceAttr(attr)
	}

	blocks := make(map[string]schemaDataSource.Block, len(resourceBlocks))
	for name, block := range resourceBlocks {
		blocks[name] = newReadOnlyDataSourceBlockFromResourceBlock(block)
	}

	return attributes, blocks
}
