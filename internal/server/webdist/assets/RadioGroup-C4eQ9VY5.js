import{a0 as _,as as p,aS as c,$ as x,cm as j,V as le,a4 as M,cC as L,p as F,cD as O,aY as $,cp as ce,a$ as K,d as W,a1 as P,cy as ue,o as I,c as D,a as G,a3 as V,al as y,ak as Y,dx as X,aK as q,au as J,g as T,aA as be,a6 as E,x as he,cn as fe,X as ve}from"./index-H5v2MVQe.js";import{g as ge}from"./Space-RXAMgD9H.js";import{u as Q}from"./use-merged-state-D31MnSsB.js";var pe=_("radio",`
 line-height: var(--n-label-line-height);
 outline: none;
 position: relative;
 user-select: none;
 -webkit-user-select: none;
 display: inline-flex;
 align-items: flex-start;
 flex-wrap: nowrap;
 font-size: var(--n-font-size);
 word-break: break-word;
`,[p("checked",[c("dot",`
 background-color: var(--n-color-active);
 `)]),c("dot-wrapper",`
 position: relative;
 flex-shrink: 0;
 flex-grow: 0;
 width: var(--n-radio-size);
 `),_("radio-input",`
 position: absolute;
 border: 0;
 width: 0;
 height: 0;
 opacity: 0;
 margin: 0;
 `),c("dot",`
 position: absolute;
 top: 50%;
 left: 0;
 transform: translateY(-50%);
 height: var(--n-radio-size);
 width: var(--n-radio-size);
 background: var(--n-color);
 box-shadow: var(--n-box-shadow);
 border-radius: 50%;
 transition:
 background-color .3s var(--n-bezier),
 box-shadow .3s var(--n-bezier);
 `,[x("&::before",`
 content: "";
 opacity: 0;
 position: absolute;
 left: 4px;
 top: 4px;
 height: calc(100% - 8px);
 width: calc(100% - 8px);
 border-radius: 50%;
 transform: scale(.8);
 background: var(--n-dot-color-active);
 transition: 
 opacity .3s var(--n-bezier),
 background-color .3s var(--n-bezier),
 transform .3s var(--n-bezier);
 `),p("checked",{boxShadow:"var(--n-box-shadow-active)"},[x("&::before",`
 opacity: 1;
 transform: scale(1);
 `)])]),c("label",`
 color: var(--n-text-color);
 padding: var(--n-label-padding);
 font-weight: var(--n-label-font-weight);
 display: inline-block;
 transition: color .3s var(--n-bezier);
 `),j("disabled",`
 cursor: pointer;
 `,[x("&:hover",[c("dot",{boxShadow:"var(--n-box-shadow-hover)"})]),p("focus",[x("&:not(:active)",[c("dot",{boxShadow:"var(--n-box-shadow-focus)"})])])]),p("disabled",`
 cursor: not-allowed;
 `,[c("dot",{boxShadow:"var(--n-box-shadow-disabled)",backgroundColor:"var(--n-color-disabled)"},[x("&::before",{backgroundColor:"var(--n-dot-color-disabled)"}),p("checked",`
 opacity: 1;
 `)]),c("label",{color:"var(--n-text-color-disabled)"}),_("radio-input",`
 cursor: not-allowed;
 `)])]);const me={name:String,value:{type:[String,Number,Boolean],default:"on"},checked:{type:Boolean,default:void 0},defaultChecked:Boolean,disabled:{type:Boolean,default:void 0},label:String,size:String,onUpdateChecked:[Function,Array],"onUpdate:checked":[Function,Array],checkedValue:{type:Boolean,default:void 0}},Z=ce("n-radio-group");function xe(o){const e=le(Z,null),{mergedClsPrefixRef:a,mergedComponentPropsRef:r}=M(o),d=L(o,{mergedSize(n){const{size:t}=o;if(t!==void 0)return t;if(e){const{mergedSizeRef:{value:g}}=e;if(g!==void 0)return g}if(n)return n.mergedSize.value;const i=r?.value?.Radio?.size;return i||"medium"},mergedDisabled(n){return!!(o.disabled||e?.disabledRef.value||n?.disabled.value)}}),{mergedSizeRef:s,mergedDisabledRef:u}=d,f=F(null),b=F(null),l=F(o.defaultChecked),h=K(o,"checked"),R=Q(h,l),v=O(()=>e?e.valueRef.value===o.value:R.value),C=O(()=>{const{name:n}=o;if(n!==void 0)return n;if(e)return e.nameRef.value}),m=F(!1);function k(){if(e){const{doUpdateValue:n}=e,{value:t}=o;$(n,t)}else{const{onUpdateChecked:n,"onUpdate:checked":t}=o,{nTriggerFormInput:i,nTriggerFormChange:g}=d;n&&$(n,!0),t&&$(t,!0),i(),g(),l.value=!0}}function w(){u.value||v.value||k()}function z(){w(),f.value&&(f.value.checked=v.value)}function S(){m.value=!1}function B(){m.value=!0}return{mergedClsPrefix:e?e.mergedClsPrefixRef:a,inputRef:f,labelRef:b,mergedName:C,mergedDisabled:u,renderSafeChecked:v,focus:m,mergedSize:s,handleRadioInputChange:z,handleRadioInputBlur:S,handleRadioInputFocus:B}}const Re=["value","name","checked","disabled","onChange","onFocus","onBlur"],Ce={...P.props,...me};var ke=W({name:"Radio",props:Ce,setup(o){const e=xe(o),a=P("Radio","-radio",pe,X,o,e.mergedClsPrefix),r=T(()=>{const{mergedSize:{value:l}}=e,{common:{cubicBezierEaseInOut:h},self:{boxShadow:R,boxShadowActive:v,boxShadowDisabled:C,boxShadowFocus:m,boxShadowHover:k,color:w,colorDisabled:z,colorActive:S,textColor:B,textColorDisabled:n,dotColorActive:t,dotColorDisabled:i,labelPadding:g,labelLineHeight:A,labelFontWeight:U,[E("fontSize",l)]:N,[E("radioSize",l)]:H}}=a.value;return{"--n-bezier":h,"--n-label-line-height":A,"--n-label-font-weight":U,"--n-box-shadow":R,"--n-box-shadow-active":v,"--n-box-shadow-disabled":C,"--n-box-shadow-focus":m,"--n-box-shadow-hover":k,"--n-color":w,"--n-color-active":S,"--n-color-disabled":z,"--n-dot-color-active":t,"--n-dot-color-disabled":i,"--n-font-size":N,"--n-radio-size":H,"--n-text-color":B,"--n-text-color-disabled":n,"--n-label-padding":g}}),{inlineThemeDisabled:d,mergedClsPrefixRef:s,mergedRtlRef:u}=M(o),f=q("Radio",u,s),b=d?J("radio",T(()=>e.mergedSize.value[0]),r,o):void 0;return Object.assign(e,{rtlEnabled:f,cssVars:d?void 0:r,themeClass:b?.themeClass,onRender:b?.onRender})},render(){const{$slots:o,mergedClsPrefix:e,onRender:a,label:r}=this;return a?.(),(()=>{const d=ue("f8c6901d8cd45c02");return I(),D("label",{class:y([`${e}-radio`,this.themeClass,this.rtlEnabled&&`${e}-radio--rtl`,this.mergedDisabled&&`${e}-radio--disabled`,this.renderSafeChecked&&`${e}-radio--checked`,this.focus&&`${e}-radio--focus`]),style:Y(this.cssVars)},[G("div",{class:y(`${e}-radio__dot-wrapper`)},[d[0]||(d[0]=V(" ",-1)),G("div",{class:y([`${e}-radio__dot`,this.renderSafeChecked&&`${e}-radio__dot--checked`])},null,2),G("input",{ref:"inputRef",type:"radio",class:y(`${e}-radio-input`),value:this.value,name:this.mergedName,checked:this.renderSafeChecked,disabled:this.mergedDisabled,onChange:this.handleRadioInputChange,onFocus:this.handleRadioInputFocus,onBlur:this.handleRadioInputBlur},null,42,Re)],2),V(()=>be(o.default,s=>!s&&!r?null:(I(),D("div",{ref:"labelRef",class:y(`${e}-radio__label`)},[V(()=>s||r)],2))))],6)})()}}),we=_("radio-group",`
 display: inline-block;
 font-size: var(--n-font-size);
`,[c("splitor",`
 display: inline-block;
 vertical-align: bottom;
 width: 1px;
 transition:
 background-color .3s var(--n-bezier),
 opacity .3s var(--n-bezier);
 background: var(--n-button-border-color);
 `,[p("checked",{backgroundColor:"var(--n-button-border-color-active)"}),p("disabled",{opacity:"var(--n-opacity-disabled)"})]),p("button-group",`
 white-space: nowrap;
 height: var(--n-height);
 line-height: var(--n-height);
 `,[_("radio-button",{height:"var(--n-height)",lineHeight:"var(--n-height)"}),c("splitor",{height:"var(--n-height)"})]),_("radio-button",`
 vertical-align: bottom;
 outline: none;
 position: relative;
 user-select: none;
 -webkit-user-select: none;
 display: inline-block;
 box-sizing: border-box;
 padding-left: 14px;
 padding-right: 14px;
 white-space: nowrap;
 transition:
 background-color .3s var(--n-bezier),
 opacity .3s var(--n-bezier),
 border-color .3s var(--n-bezier),
 color .3s var(--n-bezier);
 background: var(--n-button-color);
 color: var(--n-button-text-color);
 border-top: 1px solid var(--n-button-border-color);
 border-bottom: 1px solid var(--n-button-border-color);
 `,[_("radio-input",`
 pointer-events: none;
 position: absolute;
 border: 0;
 border-radius: inherit;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 opacity: 0;
 z-index: 1;
 `),c("state-border",`
 z-index: 1;
 pointer-events: none;
 position: absolute;
 box-shadow: var(--n-button-box-shadow);
 transition: box-shadow .3s var(--n-bezier);
 left: -1px;
 bottom: -1px;
 right: -1px;
 top: -1px;
 `),x("&:first-child",`
 border-top-left-radius: var(--n-button-border-radius);
 border-bottom-left-radius: var(--n-button-border-radius);
 border-left: 1px solid var(--n-button-border-color);
 `,[c("state-border",`
 border-top-left-radius: var(--n-button-border-radius);
 border-bottom-left-radius: var(--n-button-border-radius);
 `)]),x("&:last-child",`
 border-top-right-radius: var(--n-button-border-radius);
 border-bottom-right-radius: var(--n-button-border-radius);
 border-right: 1px solid var(--n-button-border-color);
 `,[c("state-border",`
 border-top-right-radius: var(--n-button-border-radius);
 border-bottom-right-radius: var(--n-button-border-radius);
 `)]),j("disabled",`
 cursor: pointer;
 `,[x("&:hover",[c("state-border",`
 transition: box-shadow .3s var(--n-bezier);
 box-shadow: var(--n-button-box-shadow-hover);
 `),j("checked",{color:"var(--n-button-text-color-hover)"})]),p("focus",[x("&:not(:active)",[c("state-border",{boxShadow:"var(--n-button-box-shadow-focus)"})])])]),p("checked",`
 background: var(--n-button-color-active);
 color: var(--n-button-text-color-active);
 border-color: var(--n-button-border-color-active);
 `),p("disabled",`
 cursor: not-allowed;
 opacity: var(--n-opacity-disabled);
 `)])]);const ze=["onFocusin","onFocusout"];function Se(o,e,a){const r=[];let d=!1;for(let s=0;s<o.length;++s){const u=o[s],f=u.type?.name;f==="RadioButton"&&(d=!0);const b=u.props;if(f!=="RadioButton"){r.push(u);continue}if(s===0)r.push(u);else{const l=r[r.length-1].props,h=e===l.value,R=l.disabled,v=e===b.value,C=b.disabled,m=(h?2:0)+(R?0:1),k=(v?2:0)+(C?0:1),w={[`${a}-radio-group__splitor--disabled`]:R,[`${a}-radio-group__splitor--checked`]:h},z={[`${a}-radio-group__splitor--disabled`]:C,[`${a}-radio-group__splitor--checked`]:v},S=m<k?z:w;r.push((I(),D("div",{key:1,class:y([`${a}-radio-group__splitor`,S])},null,2)),u)}}return{children:r,isButtonGroup:d}}const ye={...P.props,name:String,options:Array,labelField:{type:String,default:"label"},valueField:{type:String,default:"value"},value:[String,Number,Boolean],defaultValue:{type:[String,Number,Boolean],default:null},size:String,disabled:{type:Boolean,default:void 0},"onUpdate:value":[Function,Array],onUpdateValue:[Function,Array]};var $e=W({name:"RadioGroup",props:ye,setup(o){const e=F(null),{mergedSizeRef:a,mergedDisabledRef:r,nTriggerFormChange:d,nTriggerFormInput:s,nTriggerFormBlur:u,nTriggerFormFocus:f}=L(o),{mergedClsPrefixRef:b,inlineThemeDisabled:l,mergedRtlRef:h}=M(o),R=P("Radio","-radio-group",we,X,o,b),v=F(o.defaultValue),C=K(o,"value"),m=Q(C,v);function k(t){const{onUpdateValue:i,"onUpdate:value":g}=o;i&&$(i,t),g&&$(g,t),v.value=t,d(),s()}function w(t){const{value:i}=e;i&&(i.contains(t.relatedTarget)||f())}function z(t){const{value:i}=e;i&&(i.contains(t.relatedTarget)||u())}ve(Z,{mergedClsPrefixRef:b,nameRef:K(o,"name"),valueRef:m,disabledRef:r,mergedSizeRef:a,doUpdateValue:k});const S=q("Radio",h,b),B=T(()=>{const{value:t}=a,{common:{cubicBezierEaseInOut:i},self:{buttonBorderColor:g,buttonBorderColorActive:A,buttonBorderRadius:U,buttonBoxShadow:N,buttonBoxShadowFocus:H,buttonBoxShadowHover:ee,buttonColor:oe,buttonColorActive:te,buttonTextColor:re,buttonTextColorActive:ae,buttonTextColorHover:ne,opacityDisabled:ie,[E("buttonHeight",t)]:de,[E("fontSize",t)]:se}}=R.value;return{"--n-font-size":se,"--n-bezier":i,"--n-button-border-color":g,"--n-button-border-color-active":A,"--n-button-border-radius":U,"--n-button-box-shadow":N,"--n-button-box-shadow-focus":H,"--n-button-box-shadow-hover":ee,"--n-button-color":oe,"--n-button-color-active":te,"--n-button-text-color":re,"--n-button-text-color-hover":ne,"--n-button-text-color-active":ae,"--n-height":de,"--n-opacity-disabled":ie}}),n=l?J("radio-group",T(()=>a.value[0]),B,o):void 0;return{selfElRef:e,rtlEnabled:S,mergedClsPrefix:b,mergedValue:m,handleFocusout:z,handleFocusin:w,cssVars:l?void 0:B,themeClass:n?.themeClass,onRender:n?.onRender}},render(){const{mergedValue:o,mergedClsPrefix:e,handleFocusin:a,handleFocusout:r}=this,{options:d,labelField:s,valueField:u}=this.$props,{children:f,isButtonGroup:b}=Se(d?d.map(l=>{const h=l[u];return I(),he(ke,{key:typeof h=="boolean"?`__n_${h}`:h,value:h,disabled:l.disabled,label:l[s]},null,8,["value","disabled","label"])}):fe(ge(this)),o,e);return this.onRender?.(),I(),D("div",{onFocusin:a,onFocusout:r,ref:"selfElRef",class:y([`${e}-radio-group`,this.rtlEnabled&&`${e}-radio-group--rtl`,this.themeClass,b&&`${e}-radio-group--button-group`]),style:Y(this.cssVars)},[V(()=>f)],46,ze)}});export{$e as R,ke as a,me as r,xe as s};
