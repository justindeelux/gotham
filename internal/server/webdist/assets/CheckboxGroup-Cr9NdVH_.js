import{bG as E,a as $,V as f,W as a,ak as T,aJ as z,bu as me,cn as xe,co as ge,d as W,X as O,ar as pe,o as S,c as B,_,ad as R,e as ye,bz as Ce,bM as ze,ac as we,P as Re,$ as J,s as N,bL as Se,bK as X,aB as Te,am as _e,cp as Be,z as I,cB as $e,aP as t,a1 as j,aS as V,by as Fe,F as L,n as De,S as Me}from"./index-CmpWsR0j.js";import{u as Y}from"./Input-Co-6TWYz.js";var Ae=()=>(()=>{const e=E("75be776d8875fa17");return e[0]||(e[0]=$("svg",{viewBox:"0 0 64 64",class:"check-icon"},[$("path",{d:"M50.42,16.76L22.34,39.45l-8.1-11.46c-1.12-1.58-3.3-1.96-4.88-0.84c-1.58,1.12-1.95,3.3-0.84,4.88l10.26,14.51  c0.56,0.79,1.42,1.31,2.38,1.45c0.16,0.02,0.32,0.03,0.48,0.03c0.8,0,1.57-0.27,2.2-0.78l30.99-25.03c1.5-1.21,1.74-3.42,0.52-4.92  C54.13,15.78,51.93,15.55,50.42,16.76z"})],-1))})(),Pe=()=>(()=>{const e=E("c6eed899356c8404");return e[0]||(e[0]=$("svg",{viewBox:"0 0 100 100",class:"line-icon"},[$("path",{d:"M80.2,55.5H21.4c-2.8,0-5.1-2.5-5.1-5.5l0,0c0-3,2.3-5.5,5.1-5.5h58.7c2.8,0,5.1,2.5,5.1,5.5l0,0C85.2,53.1,82.9,55.5,80.2,55.5z"})],-1))})(),Ie=f([a("checkbox",`
 font-size: var(--n-font-size);
 outline: none;
 cursor: pointer;
 display: inline-flex;
 flex-wrap: nowrap;
 align-items: flex-start;
 word-break: break-word;
 line-height: var(--n-size);
 --n-merged-color-table: var(--n-color-table);
 `,[T("show-label","line-height: var(--n-label-line-height);"),f("&:hover",[a("checkbox-box",[z("border","border: var(--n-border-checked);")])]),f("&:focus:not(:active)",[a("checkbox-box",[z("border",`
 border: var(--n-border-focus);
 box-shadow: var(--n-box-shadow-focus);
 `)])]),T("inside-table",[a("checkbox-box",`
 background-color: var(--n-merged-color-table);
 `)]),T("checked",[a("checkbox-box",`
 background-color: var(--n-color-checked);
 `,[a("checkbox-icon",[f(".check-icon",`
 opacity: 1;
 transform: scale(1);
 `)])])]),T("indeterminate",[a("checkbox-box",[a("checkbox-icon",[f(".check-icon",`
 opacity: 0;
 transform: scale(.5);
 `),f(".line-icon",`
 opacity: 1;
 transform: scale(1);
 `)])])]),T("checked, indeterminate",[f("&:focus:not(:active)",[a("checkbox-box",[z("border",`
 border: var(--n-border-checked);
 box-shadow: var(--n-box-shadow-focus);
 `)])]),a("checkbox-box",`
 background-color: var(--n-color-checked);
 border-left: 0;
 border-top: 0;
 `,[z("border",{border:"var(--n-border-checked)"})])]),T("disabled",{cursor:"not-allowed"},[T("checked",[a("checkbox-box",`
 background-color: var(--n-color-disabled-checked);
 `,[z("border",{border:"var(--n-border-disabled-checked)"}),a("checkbox-icon",[f(".check-icon, .line-icon",{fill:"var(--n-check-mark-color-disabled-checked)"})])])]),a("checkbox-box",`
 background-color: var(--n-color-disabled);
 `,[z("border",`
 border: var(--n-border-disabled);
 `),a("checkbox-icon",[f(".check-icon, .line-icon",`
 fill: var(--n-check-mark-color-disabled);
 `)])]),z("label",`
 color: var(--n-text-color-disabled);
 `)]),a("checkbox-box-wrapper",`
 position: relative;
 width: var(--n-size);
 flex-shrink: 0;
 flex-grow: 0;
 user-select: none;
 -webkit-user-select: none;
 `),a("checkbox-box",`
 position: absolute;
 left: 0;
 top: 50%;
 transform: translateY(-50%);
 height: var(--n-size);
 width: var(--n-size);
 display: inline-block;
 box-sizing: border-box;
 border-radius: var(--n-border-radius);
 background-color: var(--n-color);
 transition: background-color 0.3s var(--n-bezier);
 `,[z("border",`
 transition:
 border-color .3s var(--n-bezier),
 box-shadow .3s var(--n-bezier);
 border-radius: inherit;
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 border: var(--n-border);
 `),a("checkbox-icon",`
 display: flex;
 align-items: center;
 justify-content: center;
 position: absolute;
 left: 1px;
 right: 1px;
 top: 1px;
 bottom: 1px;
 `,[f(".check-icon, .line-icon",`
 width: 100%;
 fill: var(--n-check-mark-color);
 opacity: 0;
 transform: scale(0.5);
 transform-origin: center;
 transition:
 fill 0.3s var(--n-bezier),
 transform 0.3s var(--n-bezier),
 opacity 0.3s var(--n-bezier),
 border-color 0.3s var(--n-bezier);
 `),me({left:"1px",top:"1px"})])]),z("label",`
 color: var(--n-text-color);
 transition: color .3s var(--n-bezier);
 user-select: none;
 -webkit-user-select: none;
 padding: var(--n-label-padding);
 font-weight: var(--n-label-font-weight);
 `,[f("&:empty",{display:"none"})])]),xe(a("checkbox",`
 --n-merged-color-table: var(--n-color-table-modal);
 `)),ge(a("checkbox",`
 --n-merged-color-table: var(--n-color-table-popover);
 `))]);const Ke=["id"],Ue=["tabindex","aria-checked","aria-labelledby","onKeyup","onKeydown","onClick"],Ne={...O.props,size:String,checked:{type:[Boolean,String,Number],default:void 0},defaultChecked:{type:[Boolean,String,Number],default:!1},value:[String,Number],disabled:{type:Boolean,default:void 0},indeterminate:Boolean,label:String,focusable:{type:Boolean,default:!0},checkedValue:{type:[Boolean,String,Number],default:!0},uncheckedValue:{type:[Boolean,String,Number],default:!1},"onUpdate:checked":[Function,Array],onUpdateChecked:[Function,Array],privateInsideTable:Boolean,onChange:[Function,Array]};var Ve=W({name:"Checkbox",props:Ne,setup(e){const n=Re(q,null),u=N(null),{mergedClsPrefixRef:b,inlineThemeDisabled:p,mergedRtlRef:y,mergedComponentPropsRef:F}=J(e),h=N(e.defaultChecked),c=V(e,"checked"),A=Y(c,h),C=Se(()=>{if(n){const o=n.valueSetRef.value;return o&&e.value!==void 0?o.has(e.value):!1}else return A.value===e.checkedValue}),w=X(e,{mergedSize(o){const{size:x}=e;if(x!==void 0)return x;if(n){const{value:s}=n.mergedSizeRef;if(s!==void 0)return s}if(o){const{mergedSize:s}=o;if(s!==void 0)return s.value}const g=F?.value?.Checkbox?.size;return g||"medium"},mergedDisabled(o){const{disabled:x}=e;if(x!==void 0)return x;if(n){if(n.disabledRef.value)return!0;const{maxRef:{value:g},checkedCountRef:s}=n;if(g!==void 0&&s.value>=g&&!C.value)return!0;const{minRef:{value:D}}=n;if(D!==void 0&&s.value<=D&&C.value)return!0}return o?o.disabled.value:!1}}),{mergedDisabledRef:r,mergedSizeRef:k}=w,l=O("Checkbox","-checkbox",Ie,$e,e,b);function i(o){if(n&&e.value!==void 0)n.toggleCheckbox(!C.value,e.value);else{const{onChange:x,"onUpdate:checked":g,onUpdateChecked:s}=e,{nTriggerFormInput:D,nTriggerFormChange:U}=w,M=C.value?e.uncheckedValue:e.checkedValue;g&&t(g,M,o),s&&t(s,M,o),x&&t(x,M,o),D(),U(),h.value=M}}function v(o){r.value||i(o)}function m(o){if(!r.value)switch(o.key){case" ":case"Enter":i(o)}}function d(o){o.key===" "&&o.preventDefault()}const P={focus:()=>{u.value?.focus()},blur:()=>{u.value?.blur()}},K=Te("Checkbox",y,b),G=I(()=>{const{value:o}=k,{common:{cubicBezierEaseInOut:x},self:{borderRadius:g,color:s,colorChecked:D,colorDisabled:U,colorTableHeader:M,colorTableHeaderModal:Q,colorTableHeaderPopover:Z,checkMarkColor:ee,checkMarkColorDisabled:oe,border:re,borderFocus:ae,borderDisabled:ne,borderChecked:ce,boxShadowFocus:le,textColor:te,textColorDisabled:ie,checkMarkColorDisabledChecked:de,colorDisabledChecked:se,borderDisabledChecked:be,labelPadding:ue,labelLineHeight:he,labelFontWeight:fe,[j("fontSize",o)]:ke,[j("size",o)]:ve}}=l.value;return{"--n-label-line-height":he,"--n-label-font-weight":fe,"--n-size":ve,"--n-bezier":x,"--n-border-radius":g,"--n-border":re,"--n-border-checked":ce,"--n-border-focus":ae,"--n-border-disabled":ne,"--n-border-disabled-checked":be,"--n-box-shadow-focus":le,"--n-color":s,"--n-color-checked":D,"--n-color-table":M,"--n-color-table-modal":Q,"--n-color-table-popover":Z,"--n-color-disabled":U,"--n-color-disabled-checked":se,"--n-text-color":te,"--n-text-color-disabled":ie,"--n-check-mark-color":ee,"--n-check-mark-color-disabled":oe,"--n-check-mark-color-disabled-checked":de,"--n-font-size":ke,"--n-label-padding":ue}}),H=p?_e("checkbox",I(()=>k.value[0]),G,e):void 0;return Object.assign(w,P,{rtlEnabled:K,selfRef:u,mergedClsPrefix:b,mergedDisabled:r,renderedChecked:C,mergedTheme:l,labelId:Be(),handleClick:v,handleKeyUp:m,handleKeyDown:d,cssVars:p?void 0:G,themeClass:H?.themeClass,onRender:H?.onRender})},render(){const{$slots:e,renderedChecked:n,mergedDisabled:u,indeterminate:b,privateInsideTable:p,cssVars:y,labelId:F,label:h,mergedClsPrefix:c,focusable:A,handleKeyUp:C,handleKeyDown:w,handleClick:r}=this;this.onRender?.();const k=pe(e.default,l=>h||l?(S(),B("span",{key:1,class:R(`${c}-checkbox__label`),id:F},[_(()=>h||l)],10,Ke)):null);return(()=>{const l=E("70be6e74cd27cb50");return S(),B("div",{ref:"selfRef",class:R([`${c}-checkbox`,this.themeClass,this.rtlEnabled&&`${c}-checkbox--rtl`,n&&`${c}-checkbox--checked`,u&&`${c}-checkbox--disabled`,b&&`${c}-checkbox--indeterminate`,p&&`${c}-checkbox--inside-table`,k&&`${c}-checkbox--show-label`]),tabindex:u||!A?void 0:0,role:"checkbox","aria-checked":b?"mixed":n,"aria-labelledby":F,style:we(y),onKeyup:C,onKeydown:w,onClick:r,onMousedown:l[0]||(l[0]=()=>{ze("selectstart",window,i=>{i.preventDefault()},{once:!0})})},[$("div",{class:R(`${c}-checkbox-box-wrapper`)},[l[1]||(l[1]=_(" ",-1)),$("div",{class:R(`${c}-checkbox-box`)},[ye(Ce,null,{default:()=>this.indeterminate?(S(),B("div",{key:"indeterminate",class:R(`${c}-checkbox-icon`)},[_(()=>Pe())],2)):(S(),B("div",{key:"check",class:R(`${c}-checkbox-icon`)},[_(()=>Ae())],2))},1024),$("div",{class:R(`${c}-checkbox-box__border`)},null,2)],2)],2),_(()=>k)],46,Ue)})()}});const q=Fe("n-checkbox-group"),Ee={min:Number,max:Number,size:String,options:Array,labelField:{type:String,default:"label"},valueField:{type:String,default:"value"},value:Array,defaultValue:{type:Array,default:null},disabled:{type:Boolean,default:void 0},"onUpdate:value":[Function,Array],onUpdateValue:[Function,Array],onChange:[Function,Array]};var je=W({name:"CheckboxGroup",props:Ee,setup(e){const{mergedClsPrefixRef:n}=J(e),u=X(e),{mergedSizeRef:b,mergedDisabledRef:p}=u,y=N(e.defaultValue),F=I(()=>e.value),h=Y(F,y),c=I(()=>h.value?.length||0),A=I(()=>Array.isArray(h.value)?new Set(h.value):new Set);function C(w,r){const{nTriggerFormInput:k,nTriggerFormChange:l}=u,{onChange:i,"onUpdate:value":v,onUpdateValue:m}=e;if(Array.isArray(h.value)){const d=Array.from(h.value),P=d.findIndex(K=>K===r);w?~P||(d.push(r),m&&t(m,d,{actionType:"check",value:r}),v&&t(v,d,{actionType:"check",value:r}),k(),l(),y.value=d,i&&t(i,d)):~P&&(d.splice(P,1),m&&t(m,d,{actionType:"uncheck",value:r}),v&&t(v,d,{actionType:"uncheck",value:r}),i&&t(i,d),y.value=d,k(),l())}else w?(m&&t(m,[r],{actionType:"check",value:r}),v&&t(v,[r],{actionType:"check",value:r}),i&&t(i,[r]),y.value=[r],k(),l()):(m&&t(m,[],{actionType:"uncheck",value:r}),v&&t(v,[],{actionType:"uncheck",value:r}),i&&t(i,[]),y.value=[],k(),l())}return Me(q,{checkedCountRef:c,maxRef:V(e,"max"),minRef:V(e,"min"),valueSetRef:A,disabledRef:p,mergedSizeRef:b,toggleCheckbox:C}),{mergedClsPrefix:n}},render(){const{options:e,labelField:n,valueField:u}=this.$props;return S(),B("div",{class:R(`${this.mergedClsPrefix}-checkbox-group`),role:"group"},[e?(S(),B(L,{key:0},[_(()=>e.map(b=>{const p=b[u];return S(),De(Ve,{key:p,value:p,disabled:b.disabled,label:b[n]},null,8,["value","disabled","label"])}))],64)):(S(),B(L,{key:1},[_(()=>this.$slots.default?.())],64))],2)}});export{Ve as C,je as a};
